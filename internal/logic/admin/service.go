// Package admin implements the admin backend interface (M3):
// ban user, precisely kick a single session, and broadcast a system notice.
//
// SSOT error codes mirror internal/logic/auth (TECH_SELECTION_AND_CONTRACTS.md §3).
// Business errors are typed (*AdminError) and mapped to HTTP status by the
// gin adapter in http.go; the envelope is {"code":0,"msg":"success","data":{...}}.
package admin

import (
	"errors"
	"runtime"
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

// BatchInspector exposes the batch queue length for stats without coupling
// the admin layer to *store.BatchWriter directly.
type BatchInspector interface {
	QueueLen() int
}

// Compile-time assertion: *store.BatchWriter satisfies BatchInspector.
var _ BatchInspector = (*store.BatchWriter)(nil)

// AdminService is the admin business logic: pure DB work plus Hub levers.
type AdminService struct {
	db        *gorm.DB
	hub       *gateway.Hub
	batch     BatchInspector
	startTime time.Time
	logger    *zap.Logger
}

// NewAdminService builds the service.
func NewAdminService(db *gorm.DB, hub *gateway.Hub, batch BatchInspector, logger *zap.Logger) *AdminService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &AdminService{db: db, hub: hub, batch: batch, startTime: time.Now(), logger: logger}
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

// SystemStats is the aggregated system snapshot returned by GetStats.
type SystemStats struct {
	Server  ServerStats  `json:"server"`
	Gateway GatewayStats `json:"gateway"`
	Store   StoreStats   `json:"store"`
	Counts  CountsStats  `json:"counts"`
}

// ServerStats describes process-level runtime info.
type ServerStats struct {
	UptimeSeconds int64       `json:"uptime_seconds"`
	StartTime     string      `json:"start_time"`
	GoVersion     string      `json:"go_version"`
	NumGoroutine  int         `json:"num_goroutine"`
	Memory        MemoryStats `json:"memory"`
}

// MemoryStats mirrors runtime.MemStats counters.
type MemoryStats struct {
	AllocBytes      uint64 `json:"alloc_bytes"`
	TotalAllocBytes uint64 `json:"total_alloc_bytes"`
	SysBytes        uint64 `json:"sys_bytes"`
	NumGC           uint32 `json:"num_gc"`
}

// GatewayStats describes the WS hub.
type GatewayStats struct {
	OnlineConnections int `json:"online_connections"`
	ShardCount        int `json:"shard_count"`
}

// StoreStats describes the async store and SQL pool.
type StoreStats struct {
	BatchQueueLen int `json:"batch_queue_len"`
	BatchQueueCap int `json:"batch_queue_cap"`
	DBOpenConns   int `json:"db_open_conns"`
	DBInUse       int `json:"db_in_use"`
	DBIdle        int `json:"db_idle"`
}

// CountsStats holds DB row counts.
type CountsStats struct {
	UsersTotal    int64 `json:"users_total"`
	UsersBanned   int64 `json:"users_banned"`
	GroupsTotal   int64 `json:"groups_total"`
	MessagesTotal int64 `json:"messages_total"`
}

// GetStats collects server/gateway/store/counts into a SystemStats snapshot.
// It never panics on nil hub/batch/db: missing components report zeros.
func (s *AdminService) GetStats() (*SystemStats, error) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	uptime := int64(0)
	startStr := ""
	if s != nil && !s.startTime.IsZero() {
		uptime = int64(time.Since(s.startTime).Seconds())
		if uptime < 0 {
			uptime = 0
		}
		startStr = s.startTime.Format(time.RFC3339)
	} else {
		startStr = time.Now().Format(time.RFC3339)
	}

	online := 0
	if s != nil && s.hub != nil {
		online = s.hub.Count()
	}

	batchLen := 0
	if s != nil && s.batch != nil {
		func() {
			defer func() { _ = recover() }()
			batchLen = s.batch.QueueLen()
		}()
	}

	openConns, inUse, idle := 0, 0, 0
	if s != nil && s.db != nil {
		if sqlDB, err := s.db.DB(); err == nil && sqlDB != nil {
			st := sqlDB.Stats()
			openConns = st.OpenConnections
			inUse = st.InUse
			idle = st.Idle
		}
	}

	var usersTotal, usersBanned, groupsTotal, messagesTotal int64
	if s != nil && s.db != nil {
		if err := s.db.Model(&store.User{}).Count(&usersTotal).Error; err != nil {
			return nil, NewAdminError(CodeInternalServer, "failed to count users")
		}
		if err := s.db.Model(&store.User{}).Where("status = ?", 2).Count(&usersBanned).Error; err != nil {
			return nil, NewAdminError(CodeInternalServer, "failed to count banned users")
		}
		if err := s.db.Model(&store.Group{}).Count(&groupsTotal).Error; err != nil {
			return nil, NewAdminError(CodeInternalServer, "failed to count groups")
		}
		if err := s.db.Model(&store.Message{}).Count(&messagesTotal).Error; err != nil {
			return nil, NewAdminError(CodeInternalServer, "failed to count messages")
		}
	}

	return &SystemStats{
		Server: ServerStats{
			UptimeSeconds: uptime,
			StartTime:     startStr,
			GoVersion:     runtime.Version(),
			NumGoroutine:  runtime.NumGoroutine(),
			Memory: MemoryStats{
				AllocBytes:      mem.Alloc,
				TotalAllocBytes: mem.TotalAlloc,
				SysBytes:        mem.Sys,
				NumGC:           mem.NumGC,
			},
		},
		Gateway: GatewayStats{
			OnlineConnections: online,
			ShardCount:        gateway.ShardCount,
		},
		Store: StoreStats{
			BatchQueueLen: batchLen,
			BatchQueueCap: 10000,
			DBOpenConns:   openConns,
			DBInUse:       inUse,
			DBIdle:        idle,
		},
		Counts: CountsStats{
			UsersTotal:    usersTotal,
			UsersBanned:   usersBanned,
			GroupsTotal:   groupsTotal,
			MessagesTotal: messagesTotal,
		},
	}, nil
}

// AdminUserItem is one user row for admin governance (safe fields only,
// no password_hash).
type AdminUserItem struct {
	UserID       string    `json:"user_id"`
	Username     string    `json:"username"`
	Nickname     string    `json:"nickname"`
	AvatarURL    string    `json:"avatar_url"`
	Signature    string    `json:"signature"`
	Role         string    `json:"role"`
	Status       int8      `json:"status"`
	TokenVersion int       `json:"token_version"`
	CreatedAt    time.Time `json:"created_at"`
}

// AdminUserListResult is the paged admin user window.
type AdminUserListResult struct {
	Total int64           `json:"total"`
	Page  int             `json:"page"`
	Limit int             `json:"limit"`
	Users []AdminUserItem `json:"users"`
}

// ListUsers returns the paged user list for admin governance, newest first
// (created_at DESC).
//
// Contract:
//   - page defaults to 1 when <= 0.
//   - limit defaults to 20 when <= 0 and is clamped to max 100.
//   - When status != nil the query filters status = ?.
//   - When keyword (trimmed, at most 32 chars) is non-empty the query
//     filters (username LIKE %keyword% OR nickname LIKE %keyword%).
//   - Soft-deleted rows are always excluded; only safe columns are
//     selected (no password_hash).
func (s *AdminService) ListUsers(page, limit int, status *int8, keyword string) (*AdminUserListResult, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	kw := strings.TrimSpace(keyword)
	if len([]rune(kw)) > 32 {
		kw = string([]rune(kw)[:32])
		kw = strings.TrimSpace(kw)
	}
	if s == nil || s.db == nil {
		return nil, NewAdminError(CodeInternalServer, "db not initialized")
	}
	base := s.db.Model(&store.User{}).Where("deleted_at IS NULL")
	if status != nil {
		base = base.Where("status = ?", *status)
	}
	if kw != "" {
		like := "%" + kw + "%"
		base = base.Where("(username LIKE ? OR nickname LIKE ?)", like, like)
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, NewAdminError(CodeInternalServer, "failed to count users")
	}
	var rows []store.User
	if err := base.
		Select("id, username, nickname, avatar_url, signature, role, status, token_version, created_at").
		Offset((page - 1) * limit).Limit(limit).Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, NewAdminError(CodeInternalServer, "failed to list users")
	}
	users := make([]AdminUserItem, 0, len(rows))
	for _, u := range rows {
		users = append(users, AdminUserItem{
			UserID:       u.ID,
			Username:     u.Username,
			Nickname:     u.Nickname,
			AvatarURL:    u.AvatarURL,
			Signature:    u.Signature,
			Role:         u.Role,
			Status:       u.Status,
			TokenVersion: u.TokenVersion,
			CreatedAt:    u.CreatedAt,
		})
	}
	return &AdminUserListResult{Total: total, Page: page, Limit: limit, Users: users}, nil
}
