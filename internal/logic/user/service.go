// Package user implements user discovery & search (GET /api/v1/users).
//
// SSOT error codes mirror internal/logic/friend (TECH_SELECTION_AND_CONTRACTS.md §3).
// Business errors are typed (*UserError) and mapped to HTTP status by the
// gin adapter in http.go; the envelope is {"code":0,"msg":"success","data":{...}}.
package user

import (
	"errors"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/store"
	"time"
)

// SSOT error codes.
const (
	CodeSuccess      = 0
	CodeParamInvalid = 10001 // ERR_PARAM_INVALID
	CodeUnauthorized = 10002 // ERR_UNAUTHORIZED
	CodeForbidden    = 10003 // ERR_FORBIDDEN
	CodeUserNotFound = 20001 // ERR_USER_NOT_FOUND
	CodeInternal     = 50001 // ERR_INTERNAL_SERVER
)

// UserError is a typed business error carrying an SSOT code.
type UserError struct {
	Code int
	Msg  string
}

func (e *UserError) Error() string { return e.Msg }

// NewUserError builds a typed SSOT error.
func NewUserError(code int, msg string) *UserError { return &UserError{Code: code, Msg: msg} }

// CodeOf unwraps the SSOT code from err; unknown errors map to 50001.
func CodeOf(err error) int {
	var ue *UserError
	if errors.As(err, &ue) {
		return ue.Code
	}
	return CodeInternal
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
	case CodeInternal:
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

// UserItem is one discoverable user (safe fields only, no password_hash).
type UserItem struct {
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Nickname  string    `json:"nickname"`
	AvatarURL string    `json:"avatar_url"`
	Signature string    `json:"signature"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// UserListResult is the paged user-discovery window.
type UserListResult struct {
	Total int64      `json:"total"`
	Page  int        `json:"page"`
	Limit int        `json:"limit"`
	Users []UserItem `json:"users"`
}

// UserService is the user-discovery business logic.
type UserService struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewUserService builds the service.
func NewUserService(db *gorm.DB, logger *zap.Logger) *UserService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &UserService{db: db, logger: logger}
}

// ListUsers returns the paged list of active users (status = 1 AND
// deleted_at IS NULL), newest first (created_at DESC).
//
// Contract:
//   - page defaults to 1 when <= 0.
//   - limit defaults to 20 when <= 0 and is clamped to max 50.
//   - keyword is trimmed; at most 32 chars are used. When non-empty the
//     query filters (username LIKE %keyword% OR nickname LIKE %keyword%).
//   - Only safe columns are selected (no password_hash).
func (s *UserService) ListUsers(page, limit int, keyword string) (*UserListResult, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	kw := strings.TrimSpace(keyword)
	if len([]rune(kw)) > 32 {
		kw = string([]rune(kw)[:32])
		kw = strings.TrimSpace(kw)
	}
	if s == nil || s.db == nil {
		return nil, NewUserError(CodeInternal, "db not initialized")
	}
	base := s.db.Model(&store.User{}).Where("status = ? AND deleted_at IS NULL", 1)
	if kw != "" {
		like := "%" + kw + "%"
		base = base.Where("(username LIKE ? OR nickname LIKE ?)", like, like)
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, NewUserError(CodeInternal, "failed to count users")
	}
	var rows []store.User
	if err := base.
		Select("id, username, nickname, avatar_url, signature, role, created_at").
		Offset((page - 1) * limit).Limit(limit).Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, NewUserError(CodeInternal, "failed to list users")
	}
	users := make([]UserItem, 0, len(rows))
	for _, u := range rows {
		users = append(users, UserItem{
			UserID:    u.ID,
			Username:  u.Username,
			Nickname:  u.Nickname,
			AvatarURL: u.AvatarURL,
			Signature: u.Signature,
			Role:      u.Role,
			CreatedAt: u.CreatedAt,
		})
	}
	return &UserListResult{Total: total, Page: page, Limit: limit, Users: users}, nil
}
