// Package auth implements the account & security layer (Stage-1 M2).
//
// SSOT: auth endpoints contract + response envelope and the shared
// error-code table; the users / user_sessions schemas; layer-4 domain
// service boundaries.
//
// service.go holds the pure business logic: no gin imports here. The HTTP
// adapter lives in http.go; the one-time WS handshake ticket in ticket.go.
package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/config"
	"golang-im-neo-system/internal/store"
	"golang-im-neo-system/pkg/util"
)

// SSOT error codes (TECH_SELECTION_AND_CONTRACTS.md §3).
const (
	CodeSuccess           = 0
	CodeParamInvalid      = 10001 // ERR_PARAM_INVALID
	CodeUnauthorized      = 10002 // ERR_UNAUTHORIZED
	CodeForbidden         = 10003 // ERR_FORBIDDEN
	CodeUserNotFound      = 20001 // ERR_USER_NOT_FOUND
	CodeUserExisted       = 20002 // ERR_USER_EXISTED
	CodePasswordIncorrect = 20003 // ERR_PASSWORD_INCORRECT
	CodeUserBanned        = 20004 // ERR_USER_BANNED
	CodeInternalServer    = 50001 // ERR_INTERNAL_SERVER
)

// Default TTLs when the security config carries no (parsable) value.
// AT 2h + RT 14d per the M2 contract.
const (
	DefaultAccessTTL  = 2 * time.Hour
	DefaultRefreshTTL = 14 * 24 * time.Hour
)

var dummyBcryptHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// AuthError is a typed business error carrying an SSOT code.
// The HTTP adapter (http.go) maps it into the unified envelope.
type AuthError struct {
	Code int
	Msg  string
}

func (e *AuthError) Error() string { return e.Msg }

// NewAuthError builds a typed SSOT error.
func NewAuthError(code int, msg string) *AuthError { return &AuthError{Code: code, Msg: msg} }

// CodeOf unwraps the SSOT code from err; unknown errors map to 50001.
func CodeOf(err error) int {
	var ae *AuthError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return CodeInternalServer
}

// HTTPStatusOf maps an SSOT code to a proper HTTP status.
// A non-zero business code is NEVER returned with HTTP 200.
func HTTPStatusOf(code int) int {
	switch code {
	case CodeSuccess:
		return 200
	case CodeParamInvalid:
		return 400
	case CodeUnauthorized:
		return 401
	case CodeForbidden:
		return 403
	case CodeUserNotFound:
		return 404
	case CodeUserExisted:
		return 409
	case CodePasswordIncorrect:
		return 401
	case CodeUserBanned:
		return 403
	case CodeInternalServer:
		return 500
	default:
		if code >= 50000 {
			return 500
		}
		if code >= 40000 {
			return 400
		}
		return 400
	}
}

// AuthService is the pure account/session business logic.
type AuthService struct {
	db  *gorm.DB
	cfg *config.Config
}

// NewAuthService builds the service. cfg must carry the [security] block
// (JWTSecret, JWTAccessExpire, JWTRefreshExpire, BcryptCost).
func NewAuthService(db *gorm.DB, cfg *config.Config) *AuthService {
	return &AuthService{db: db, cfg: cfg}
}

func (s *AuthService) accessTTL() time.Duration {
	if s.cfg != nil && s.cfg.Security.JWTAccessExpire != "" {
		if d, err := time.ParseDuration(s.cfg.Security.JWTAccessExpire); err == nil && d > 0 {
			return d
		}
	}
	return DefaultAccessTTL
}

func (s *AuthService) refreshTTL() time.Duration {
	if s.cfg != nil && s.cfg.Security.JWTRefreshExpire != "" {
		if d, err := time.ParseDuration(s.cfg.Security.JWTRefreshExpire); err == nil && d > 0 {
			return d
		}
	}
	return DefaultRefreshTTL
}

func (s *AuthService) jwtSecret() []byte {
	if s.cfg != nil {
		return []byte(s.cfg.Security.JWTSecret)
	}
	return nil
}

// sha256Hex hashes a refresh token / ticket secret for storage.
// Raw refresh tokens never touch the DB — only their SHA256 hex digest.
func sha256Hex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") ||
		strings.Contains(msg, "unique") ||
		strings.Contains(msg, "Duplicate entry")
}

// signAccessToken issues an HS256 access token with the EXACT pinned claim
// keys: user_id, username, role, session_id, device_class, tv (+ iat/exp).
func (s *AuthService) signAccessToken(user *store.User, sessionID, deviceClass string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"user_id":      user.ID,
		"username":     user.Username,
		"role":         user.Role,
		"session_id":   sessionID,
		"device_class": deviceClass,
		"tv":           user.TokenVersion,
		"iat":          now.Unix(),
		"exp":          now.Add(s.accessTTL()).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(s.jwtSecret())
	if err != nil {
		return "", NewAuthError(CodeInternalServer, "failed to sign access token")
	}
	return signed, nil
}

// Register creates a new user. Validation failures → 10001,
// duplicate username → 20002.
func (s *AuthService) Register(username, password string) (string, error) {
	username = strings.TrimSpace(username)
	if n := utf8.RuneCountInString(username); n < 2 || n > 32 {
		return "", NewAuthError(CodeParamInvalid, "invalid username: must be 2..32 chars")
	}
	if len(password) < 8 {
		return "", NewAuthError(CodeParamInvalid, "invalid password: must be at least 8 chars")
	}

	cost := 0
	if s.cfg != nil {
		cost = s.cfg.Security.BcryptCost
	}
	hash, err := util.HashPassword(password, cost)
	if err != nil {
		return "", NewAuthError(CodeInternalServer, "failed to hash password")
	}

	user := &store.User{
		ID:           util.NewUserID(),
		Username:     username,
		PasswordHash: hash,
		Role:         "user",
		Status:       1,
		TokenVersion: 1,
	}
	if err := s.db.Create(user).Error; err != nil {
		if isUniqueViolation(err) {
			return "", NewAuthError(CodeUserExisted, "username already exists")
		}
		return "", NewAuthError(CodeInternalServer, "failed to create user")
	}
	return user.ID, nil
}

// Login authenticates a user and opens a new session row.
// Returns (accessToken, refreshToken, userID, sessionID, err) with typed codes:
// missing user and wrong password both → 20003 (enumeration/timing protection),
// banned → 20004.
func (s *AuthService) Login(username, password, deviceClass, deviceName string) (string, string, string, string, error) {
	fail := func(err error) (string, string, string, string, error) {
		return "", "", "", "", err
	}

	var user store.User
	if err := s.db.Where("username = ?", strings.TrimSpace(username)).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = bcrypt.CompareHashAndPassword([]byte(dummyBcryptHash), []byte(password))
			return fail(NewAuthError(CodePasswordIncorrect, "invalid username or password"))
		}
		return fail(NewAuthError(CodeInternalServer, "failed to load user"))
	}
	if user.Status != 1 {
		return fail(NewAuthError(CodeUserBanned, "user account is banned"))
	}
	if !util.CheckPassword(user.PasswordHash, password) {
		return fail(NewAuthError(CodePasswordIncorrect, "invalid username or password"))
	}

	if deviceClass == "" {
		deviceClass = "interactive"
	}
	rt := "rt_" + uuid.NewString()
	now := time.Now()
	sess := &store.UserSession{
		ID:               util.NewSessionID(),
		UserID:           user.ID,
		DeviceClass:      deviceClass,
		DeviceName:       deviceName,
		RefreshTokenHash: sha256Hex(rt),
		TokenVersion:     user.TokenVersion,
		ExpiresAt:        now.Add(s.refreshTTL()),
		LastActiveAt:     now,
		CreatedAt:        now,
	}
	if err := s.db.Create(sess).Error; err != nil {
		return fail(NewAuthError(CodeInternalServer, "failed to create session"))
	}
	at, err := s.signAccessToken(&user, sess.ID, deviceClass)
	if err != nil {
		return fail(err)
	}
	return at, rt, user.ID, sess.ID, nil
}

// Refresh rotates a refresh token single-use and returns a fresh pair.
// Missing/revoked/expired session, hash mismatch, or a lost compare-and-swap
// race (concurrent replay) all → 10002. Banned user → 20004.
// The fresh AT is signed with the user's CURRENT TokenVersion (re-read).
func (s *AuthService) Refresh(refreshToken, sessionID string) (string, string, error) {
	if refreshToken == "" || sessionID == "" {
		return "", "", NewAuthError(CodeUnauthorized, "invalid session")
	}

	var sess store.UserSession
	if err := s.db.Where("id = ?", sessionID).First(&sess).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", NewAuthError(CodeUnauthorized, "invalid session")
		}
		return "", "", NewAuthError(CodeInternalServer, "failed to load session")
	}
	if sess.IsRevoked != 0 {
		return "", "", NewAuthError(CodeUnauthorized, "session revoked")
	}
	if time.Now().After(sess.ExpiresAt) {
		return "", "", NewAuthError(CodeUnauthorized, "session expired")
	}
	if sha256Hex(refreshToken) != sess.RefreshTokenHash {
		return "", "", NewAuthError(CodeUnauthorized, "invalid refresh token")
	}

	var user store.User
	if err := s.db.Where("id = ?", sess.UserID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", NewAuthError(CodeUnauthorized, "invalid session")
		}
		return "", "", NewAuthError(CodeInternalServer, "failed to load user")
	}
	if user.Status != 1 {
		return "", "", NewAuthError(CodeUserBanned, "user account is banned")
	}

	newRT := "rt_" + uuid.NewString()
	newHash := sha256Hex(newRT)
	now := time.Now()
	// Single-use rotation with concurrent-replay protection: the UPDATE only
	// succeeds if the stored hash still matches the one we verified above.
	res := s.db.Model(&store.UserSession{}).
		Where("id = ? AND refresh_token_hash = ?", sess.ID, sess.RefreshTokenHash).
		Updates(map[string]interface{}{
			"refresh_token_hash": newHash,
			"last_active_at":     now,
		})
	if res.Error != nil {
		return "", "", NewAuthError(CodeInternalServer, "failed to rotate session")
	}
	if res.RowsAffected == 0 {
		return "", "", NewAuthError(CodeUnauthorized, "refresh token already used")
	}

	newAT, err := s.signAccessToken(&user, sess.ID, sess.DeviceClass)
	if err != nil {
		return "", "", err
	}
	return newAT, newRT, nil
}
