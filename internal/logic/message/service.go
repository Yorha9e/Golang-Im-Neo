// Package message implements the message-history roaming business logic.
//
// Endpoints (handoff ho_7eb89ce583c9):
//
//	GET /api/v1/messages/history?cov_id=...&before_seq=...&limit=...
//	GET /api/v1/messages/private/:target_user_id/history
//	GET /api/v1/messages/hall/history
//
// This file holds the pure business logic: no gin imports here. The HTTP
// adapter lives in http.go.
package message

import (
	"errors"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/store"
)

// SSOT error codes (TECH_SELECTION_AND_CONTRACTS.md §3).
const (
	CodeSuccess        = 0
	CodeParamInvalid   = 10001 // ERR_PARAM_INVALID
	CodeUnauthorized   = 10002 // ERR_UNAUTHORIZED
	CodeForbidden      = 10003 // ERR_FORBIDDEN
	CodeFriendNotFound = 30001 // ERR_FRIEND_NOT_FOUND
	CodeInternal       = 50001 // ERR_INTERNAL_SERVER
)

// Hall conversation ids.
const (
	// PublicHallCovID is the canonical public-hall conversation id.
	PublicHallCovID = "cov:public:hall"
	// LegacyHallCovID is the legacy alias accepted for the public hall.
	LegacyHallCovID = "cov:hall:public"
)

// Pagination bounds.
const (
	DefaultLimit = 50
	MaxLimit     = 100
)

// MessageError is a typed business error carrying an SSOT code.
type MessageError struct {
	Code int
	Msg  string
}

func (e *MessageError) Error() string { return e.Msg }

// NewMessageError builds a typed SSOT error.
func NewMessageError(code int, msg string) *MessageError { return &MessageError{Code: code, Msg: msg} }

// CodeOf unwraps the SSOT code from err; unknown errors map to 50001.
func CodeOf(err error) int {
	var me *MessageError
	if errors.As(err, &me) {
		return me.Code
	}
	return CodeInternal
}

// HTTPStatusOf maps an SSOT code to a proper HTTP status.
// A non-zero business code is NEVER returned with HTTP 200.
// NOTE: CodeFriendNotFound (30001) maps to 403 here (per the message-history
// contract: non-friend private roaming is 403/30001), unlike the friend
// package where it maps to 404.
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
	case CodeFriendNotFound:
		return 403
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

// HistoryMessage is one chat record in the history window (ascending seq order).
type HistoryMessage struct {
	Seq         int64  `json:"seq"`
	FromUID     string `json:"from_uid"`
	ToUID       string `json:"to_uid"`
	Content     string `json:"content"`
	Extra       string `json:"extra"`
	Timestamp   int64  `json:"timestamp"`
	StanzaID    string `json:"stanza_id"`
	ContentType int8   `json:"content_type"`
}

// HistoryResult is the paged history window.
type HistoryResult struct {
	Messages []HistoryMessage `json:"messages"`
	HasMore  bool             `json:"has_more"`
}

// MessageService is the pure message-history business logic.
type MessageService struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewMessageService builds the service. A nil logger becomes zap.NewNop().
func NewMessageService(db *gorm.DB, logger *zap.Logger) *MessageService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MessageService{db: db, logger: logger}
}

// IsHallCovID reports whether covID is the public hall (canonical or alias).
func IsHallCovID(covID string) bool {
	return covID == PublicHallCovID || covID == LegacyHallCovID
}

// NormalizeCovID maps the legacy hall alias to the canonical id.
// All other ids are returned unchanged.
func NormalizeCovID(covID string) string {
	if covID == LegacyHallCovID {
		return PublicHallCovID
	}
	return covID
}

// parsePrivateCovID splits a private cov_id "cov:uidA:uidB" into its two uids.
// It returns ok=false for hall ids, group ids (cov:grp:...), malformed ids,
// or ids with empty parts.
func parsePrivateCovID(covID string) (uidA, uidB string, ok bool) {
	if IsHallCovID(covID) {
		return "", "", false
	}
	if strings.HasPrefix(covID, "cov:grp:") {
		return "", "", false
	}
	if !strings.HasPrefix(covID, "cov:") {
		return "", "", false
	}
	parts := strings.Split(covID, ":")
	if len(parts) != 3 {
		return "", "", false
	}
	if parts[0] != "cov" {
		return "", "", false
	}
	a := strings.TrimSpace(parts[1])
	b := strings.TrimSpace(parts[2])
	if a == "" || b == "" {
		return "", "", false
	}
	return a, b, true
}

// clampLimit normalizes the limit to [1,100], defaulting non-positive to 50.
func clampLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

// GetHistory returns the history window for covID as seen by me.
//
// Contract:
//   - me and covID are required (me missing -> 10002, covID missing -> 10001).
//   - The legacy hall alias is normalized to the canonical id.
//   - Hall (canonical or alias) needs no further checks.
//   - Private cov:uidA:uidB requires me == uidA || me == uidB (else 10003)
//     and a live mutual accepted friendship (else 30001). Self-notes
//     (uidA == uidB) skip the friendship check, mirroring the router.
//   - Group covs (cov:grp:...) are queried without friendship checks.
//   - Any other malformed cov_id maps to 10001.
//   - DB: WHERE cov_id = ? [AND seq < before_seq] ORDER BY seq DESC
//     LIMIT limit+1 (hall queries both hall ids so alias and canonical stay
//     interchangeable); rows are sliced to limit, has_more is set, and the
//     returned window is strictly ascending (oldest to newest).
func (s *MessageService) GetHistory(me, covID string, beforeSeq int64, limit int) (*HistoryResult, error) {
	me = strings.TrimSpace(me)
	if me == "" {
		return nil, NewMessageError(CodeUnauthorized, "missing auth context")
	}
	covID = strings.TrimSpace(covID)
	if covID == "" {
		return nil, NewMessageError(CodeParamInvalid, "cov_id is required")
	}
	limit = clampLimit(limit)
	if beforeSeq < 0 {
		beforeSeq = 0
	}

	// Hall: no security checks; query both ids so the alias and the
	// canonical id stay interchangeable regardless of which id rows were
	// physically written under.
	if IsHallCovID(covID) {
		return s.queryHall(beforeSeq, limit)
	}

	// Private chat: participant + friendship checks.
	if uidA, uidB, ok := parsePrivateCovID(covID); ok {
		if me != uidA && me != uidB {
			return nil, NewMessageError(CodeForbidden, "forbidden: not a conversation member")
		}
		if uidA != uidB {
			var cnt int64
			if err := s.db.Model(&store.Friendship{}).
				Where("(user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?)", uidA, uidB, uidB, uidA).
				Where("status = 'accepted'").
				Where("deleted_at IS NULL").
				Count(&cnt).Error; err != nil {
				return nil, NewMessageError(CodeInternal, "failed to check friendship")
			}
			if cnt == 0 {
				return nil, NewMessageError(CodeFriendNotFound, "not friends")
			}
		}
		return s.queryCov(covID, beforeSeq, limit)
	}

	// Group conversation: allow roaming without friendship checks
	// (membership is enforced by the group history endpoint; here we just
	// serve the window).
	if strings.HasPrefix(covID, "cov:grp:") {
		rest := strings.TrimPrefix(covID, "cov:grp:")
		if strings.TrimSpace(rest) == "" || strings.Contains(rest, ":") {
			return nil, NewMessageError(CodeParamInvalid, "invalid cov_id")
		}
		return s.queryCov(covID, beforeSeq, limit)
	}

	return nil, NewMessageError(CodeParamInvalid, "invalid cov_id")
}

// queryCov runs the canonical window query for a single cov_id.
func (s *MessageService) queryCov(covID string, beforeSeq int64, limit int) (*HistoryResult, error) {
	q := s.db.Model(&store.Message{}).Where("cov_id = ?", covID)
	if beforeSeq > 0 {
		q = q.Where("seq < ?", beforeSeq)
	}
	var rows []store.Message
	if err := q.Order("seq DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, NewMessageError(CodeInternal, "failed to load history")
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	msgs := make([]HistoryMessage, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		m := rows[i]
		msgs = append(msgs, HistoryMessage{
			Seq:         m.Seq,
			FromUID:     m.FromUID,
			ToUID:       m.ToUID,
			Content:     m.Content,
			Extra:       m.Extra,
			Timestamp:   m.Timestamp,
			StanzaID:    m.StanzaID,
			ContentType: m.ContentType,
		})
	}
	return &HistoryResult{Messages: msgs, HasMore: hasMore}, nil
}

// queryHall runs the window query over both hall ids (canonical + alias).
func (s *MessageService) queryHall(beforeSeq int64, limit int) (*HistoryResult, error) {
	q := s.db.Model(&store.Message{}).Where("cov_id IN ?", []string{PublicHallCovID, LegacyHallCovID})
	if beforeSeq > 0 {
		q = q.Where("seq < ?", beforeSeq)
	}
	var rows []store.Message
	if err := q.Order("seq DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, NewMessageError(CodeInternal, "failed to load history")
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	msgs := make([]HistoryMessage, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		m := rows[i]
		msgs = append(msgs, HistoryMessage{
			Seq:         m.Seq,
			FromUID:     m.FromUID,
			ToUID:       m.ToUID,
			Content:     m.Content,
			Extra:       m.Extra,
			Timestamp:   m.Timestamp,
			StanzaID:    m.StanzaID,
			ContentType: m.ContentType,
		})
	}
	return &HistoryResult{Messages: msgs, HasMore: hasMore}, nil
}
