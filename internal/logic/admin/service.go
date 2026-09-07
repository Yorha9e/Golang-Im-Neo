// Package admin implements the admin backend interface (M3):
// ban user, precisely kick a single session, and broadcast a system notice.
//
// SSOT error codes mirror internal/logic/auth (TECH_SELECTION_AND_CONTRACTS.md §3).
// Business errors are typed (*AdminError) and mapped to HTTP status by the
// gin adapter in http.go; the envelope is {"code":0,"msg":"success","data":{...}}.
package admin

import (
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/gateway"
	"golang-im-neo-system/internal/store"
	pb "golang-im-neo-system/proto"
)

// SSOT error codes (TECH_SELECTION_AND_CONTRACTS.md §3).
const (
	CodeSuccess        = 0
	CodeParamInvalid   = 10001 // ERR_PARAM_INVALID
	CodeUnauthorized   = 10002 // ERR_UNAUTHORIZED
	CodeForbidden      = 10003 // ERR_FORBIDDEN
	CodeUserNotFound   = 20001 // ERR_USER_NOT_FOUND
	CodeUserBanned     = 20004 // ERR_USER_BANNED
	CodeInternalServer = 50001 // ERR_INTERNAL_SERVER
)

// AdminError is a typed business error carrying an SSOT code.
type AdminError struct {
	Code int
	Msg  string
}

func (e *AdminError) Error() string { return e.Msg }

// NewAdminError builds a typed SSOT error.
func NewAdminError(code int, msg string) *AdminError { return &AdminError{Code: code, Msg: msg} }

// CodeOf unwraps the SSOT code from err; unknown errors map to 50001.
func CodeOf(err error) int {
	var ae *AdminError
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

// AdminService is the admin business logic: pure DB work plus Hub levers.
type AdminService struct {
	db     *gorm.DB
	hub    *gateway.Hub
	logger *zap.Logger
}

// NewAdminService builds the service.
func NewAdminService(db *gorm.DB, hub *gateway.Hub, logger *zap.Logger) *AdminService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &AdminService{db: db, hub: hub, logger: logger}
}

// BanUser bans a user: status=2 and token_version+1 in a tx, then drops ALL
// of that user's connections via hub.KickUser. The token_version bump
// invalidates every outstanding access token (middleware tv check) and
// status=2 blocks re-login, so the user is disconnected and cannot reconnect.
// Unknown user → 20001.
func (s *AdminService) BanUser(userID string) error {
	if strings.TrimSpace(userID) == "" {
		return NewAdminError(CodeParamInvalid, "user_id is required")
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&store.User{}).
			Where("id = ?", userID).
			Updates(map[string]interface{}{
				"status":        2,
				"token_version": gorm.Expr("token_version + 1"),
			})
		if res.Error != nil {
			return NewAdminError(CodeInternalServer, "failed to ban user")
		}
		if res.RowsAffected == 0 {
			return NewAdminError(CodeUserNotFound, "user not found")
		}
		return nil
	}); err != nil {
		return err
	}
	s.logger.Info("admin ban user", zap.String("user", userID))
	if s.hub != nil {
		s.hub.KickUser(userID, "banned")
	}
	return nil
}

// KickSession precisely kicks ONE session: marks that user_sessions row
// revoked (is_revoked=1, token_version bump) and drops only that connection
// via hub.KickSession. Other sessions of the same user (e.g. an ESP32-C3
// hardware session) stay online. Missing session → 20001.
func (s *AdminService) KickSession(sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return NewAdminError(CodeParamInvalid, "session_id is required")
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		var sess store.UserSession
		if err := tx.Where("id = ?", sessionID).First(&sess).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewAdminError(CodeUserNotFound, "session not found")
			}
			return NewAdminError(CodeInternalServer, "failed to load session")
		}
		if err := tx.Model(&store.UserSession{}).
			Where("id = ?", sessionID).
			Updates(map[string]interface{}{
				"is_revoked":    1,
				"token_version": gorm.Expr("token_version + 1"),
			}).Error; err != nil {
			return NewAdminError(CodeInternalServer, "failed to revoke session")
		}
		return nil
	}); err != nil {
		return err
	}
	s.logger.Info("admin kick session", zap.String("session", sessionID))
	if s.hub != nil {
		s.hub.KickSession(sessionID, "admin kick")
	}
	return nil
}

// Broadcast fans a system notice out to ALL connected clients.
func (s *AdminService) Broadcast(content string) error {
	if strings.TrimSpace(content) == "" {
		return NewAdminError(CodeParamInvalid, "content is required")
	}
	msg := &pb.WsMessage{
		Type:      pb.MsgType_SYSTEM_NOTICE,
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
		Extra:     `{"code":0}`,
	}
	frame, err := proto.Marshal(msg)
	if err != nil {
		return NewAdminError(CodeInternalServer, "failed to marshal broadcast")
	}
	s.logger.Info("admin broadcast", zap.Int("bytes", len(frame)))
	if s.hub != nil {
		s.hub.Broadcast(frame)
	}
	return nil
}
