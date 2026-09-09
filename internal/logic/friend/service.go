// Package friend implements the friend-system business logic (M1).
//
// SSOT: docs/TECH_SELECTION_AND_CONTRACTS.md §2.2 (endpoints) and §3
// (error codes); storage is the physical bidirectional double-row
// friendships table (internal/store.Friendship).
//
// This file holds the pure business logic: no gin imports here. The HTTP
// adapter lives in http.go.
package friend

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/logic/session"
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
	CodeFriendNotFound = 30001 // ERR_FRIEND_NOT_FOUND
	CodeFriendBlocked  = 30002 // ERR_FRIEND_BLOCKED
	CodeFriendAlready  = 30003 // ERR_FRIEND_ALREADY
	CodeInternal       = 50001 // ERR_INTERNAL_SERVER
)

// FriendError is a typed business error carrying an SSOT code.
type FriendError struct {
	Code int
	Msg  string
}

func (e *FriendError) Error() string { return e.Msg }

// NewFriendError builds a typed SSOT error.
func NewFriendError(code int, msg string) *FriendError { return &FriendError{Code: code, Msg: msg} }

// CodeOf unwraps the SSOT code from err; unknown errors map to 50001.
func CodeOf(err error) int {
	var fe *FriendError
	if errors.As(err, &fe) {
		return fe.Code
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
	case CodeFriendNotFound:
		return 404
	case CodeFriendBlocked:
		return 403
	case CodeFriendAlready:
		return 409
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

// Emitter pushes a marshaled WsMessage to all of a user's online sessions.
// Satisfied by *gateway.Hub.
type Emitter interface{ SendToUser(userID string, msg []byte) bool }

// Invalidator drops a cached friendship verdict. Satisfied by *router.Router.
type Invalidator interface{ Invalidate(covKey string) }

// FriendService is the pure friend business logic.
type FriendService struct {
	db          *gorm.DB
	emitter     Emitter
	invalidator Invalidator
	logger      *zap.Logger
}

// NewFriendService builds the service. emitter/invalidator may be nil
// (treated as no-op) so unit tests can pass fakes.
func NewFriendService(db *gorm.DB, emitter Emitter, invalidator Invalidator, logger *zap.Logger) *FriendService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &FriendService{db: db, emitter: emitter, invalidator: invalidator, logger: logger}
}

// FriendItem is one accepted friend with joined user profile fields.
type FriendItem struct {
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url"`
	Signature string `json:"signature"`
	Remark    string `json:"remark"`
}

// PendingItem is one incoming pending application with requester info.
type PendingItem struct {
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Remark    string    `json:"remark"`
	CreatedAt time.Time `json:"created_at"`
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

func (s *FriendService) invalidatePair(a, b string) {
	if s.invalidator == nil {
		return
	}
	s.invalidator.Invalidate(session.BuildPrivateCovID(a, b))
}

func (s *FriendService) emitTo(userID string, msg *pb.WsMessage) {
	if s.emitter == nil {
		return
	}
	b, err := proto.Marshal(msg)
	if err != nil {
		s.logger.Warn("friend: marshal notify failed", zap.Error(err))
		return
	}
	s.emitter.SendToUser(userID, b)
}

func (s *FriendService) emitApply(applicantID, applicantUsername, targetID, remark string) {
	content := applicantUsername
	if strings.TrimSpace(remark) != "" {
		content = applicantUsername + ": " + remark
	}
	extra, _ := json.Marshal(map[string]string{
		"from_username": applicantUsername,
		"remark":        remark,
	})
	s.emitTo(targetID, &pb.WsMessage{
		Type:      pb.MsgType_FRIEND_APPLY_NOTIFY,
		FromUid:   applicantID,
		ToUid:     targetID,
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
		Extra:     string(extra),
	})
}

func (s *FriendService) emitAccept(responderID, responderUsername, initiatorID string) {
	extra, _ := json.Marshal(map[string]string{
		"from_username": responderUsername,
	})
	s.emitTo(initiatorID, &pb.WsMessage{
		Type:      pb.MsgType_FRIEND_ACCEPT_NOTIFY,
		FromUid:   responderID,
		ToUid:     initiatorID,
		Content:   responderUsername,
		Timestamp: time.Now().UnixMilli(),
		Extra:     string(extra),
	})
}

// ListFriends returns my accepted friends (user_id=me AND status='accepted'
// AND deleted_at IS NULL) joined with users for profile fields.
func (s *FriendService) ListFriends(userID string) ([]FriendItem, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, NewFriendError(CodeParamInvalid, "missing user_id")
	}
	type row struct {
		FriendID  string
		Remark    string
		Username  string
		AvatarURL string
		Signature string
	}
	var rows []row
	if err := s.db.Table("friendships f").
		Select("f.friend_id, f.remark, u.username, u.avatar_url, u.signature").
		Joins("JOIN users u ON u.id = f.friend_id").
		Where("f.user_id = ? AND f.status = 'accepted' AND f.deleted_at IS NULL AND u.deleted_at IS NULL", userID).
		Scan(&rows).Error; err != nil {
		return nil, NewFriendError(CodeInternal, "failed to list friends")
	}
	out := make([]FriendItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, FriendItem{
			UserID:    r.FriendID,
			Username:  r.Username,
			AvatarURL: r.AvatarURL,
			Signature: r.Signature,
			Remark:    r.Remark,
		})
	}
	return out, nil
}

// ListPending returns incoming pending applications I can respond to
// (friend_id=me AND status='pending' AND deleted_at IS NULL) joined with
// the requester username.
func (s *FriendService) ListPending(userID string) ([]PendingItem, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, NewFriendError(CodeParamInvalid, "missing user_id")
	}
	type row struct {
		UserID    string
		Remark    string
		Username  string
		CreatedAt time.Time
	}
	var rows []row
	if err := s.db.Table("friendships f").
		Select("f.user_id, f.remark, u.username, f.created_at").
		Joins("JOIN users u ON u.id = f.user_id").
		Where("f.friend_id = ? AND f.initiator_id != ? AND f.status = 'pending' AND f.deleted_at IS NULL AND u.deleted_at IS NULL", userID, userID).
		Scan(&rows).Error; err != nil {
		return nil, NewFriendError(CodeInternal, "failed to list pending")
	}
	out := make([]PendingItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, PendingItem{
			UserID:    r.UserID,
			Username:  r.Username,
			Remark:    r.Remark,
			CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

// Apply creates (or revives) a bidirectional pending application A→B.
// Returns the target user ID on success.
func (s *FriendService) Apply(applicantID, targetUsername, remark string) (string, error) {
	targetUsername = strings.TrimSpace(targetUsername)
	if strings.TrimSpace(applicantID) == "" || targetUsername == "" {
		return "", NewFriendError(CodeParamInvalid, "target_username is required")
	}
	var target store.User
	if err := s.db.Where("username = ?", targetUsername).First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", NewFriendError(CodeUserNotFound, "user not found")
		}
		return "", NewFriendError(CodeInternal, "failed to load user")
	}
	targetID := target.ID
	if applicantID == targetID {
		return "", NewFriendError(CodeParamInvalid, "cannot add yourself")
	}
	var applicant store.User
	if err := s.db.Where("id = ?", applicantID).First(&applicant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", NewFriendError(CodeUserNotFound, "user not found")
		}
		return "", NewFriendError(CodeInternal, "failed to load user")
	}

	// Guard live verdicts BEFORE reviving: blocked → 30002, pending/accepted → 30003.
	var live []store.Friendship
	if err := s.db.Where(
		"(user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?)",
		applicantID, targetID, targetID, applicantID,
	).Where("deleted_at IS NULL").Find(&live).Error; err != nil {
		return "", NewFriendError(CodeInternal, "failed to check friendship")
	}
	for _, r := range live {
		if r.Status == "blocked" {
			return "", NewFriendError(CodeFriendBlocked, "blocked by user")
		}
	}
	for _, r := range live {
		if r.Status == "accepted" || r.Status == "pending" {
			return "", NewFriendError(CodeFriendAlready, "already friends or pending")
		}
	}

	now := time.Now()
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		dirs := [2][2]string{{applicantID, targetID}, {targetID, applicantID}}
		for _, d := range dirs {
			uid, fid := d[0], d[1]
			var row store.Friendship
			err := tx.Unscoped().Where("user_id = ? AND friend_id = ?", uid, fid).First(&row).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					nr := store.Friendship{
						UserID:      uid,
						FriendID:    fid,
						InitiatorID: applicantID,
						Status:      "pending",
						Remark:      remark,
						CreatedAt:   now,
						UpdatedAt:   now,
					}
					if err := tx.Create(&nr).Error; err != nil {
						return err
					}
					continue
				}
				return err
			}
			// Revive: soft-deleted (any status) or live rejected → pending.
			if err := tx.Unscoped().Model(&store.Friendship{}).
				Where("user_id = ? AND friend_id = ?", uid, fid).
				Updates(map[string]interface{}{
					"status":       "pending",
					"initiator_id": applicantID,
					"remark":       remark,
					"updated_at":   now,
					"deleted_at":   nil,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		var fe *FriendError
		if errors.As(err, &fe) {
			return "", err
		}
		if isUniqueViolation(err) {
			return "", NewFriendError(CodeFriendAlready, "already friends or pending")
		}
		return "", NewFriendError(CodeInternal, "failed to apply")
	}

	s.invalidatePair(applicantID, targetID)
	s.emitApply(applicantID, applicant.Username, targetID, remark)
	return targetID, nil
}

// Respond accepts or rejects a pending application. initiatorID is the
// INITIATOR of the pending application; responderID is the current user and
// must be the non-initiator.
func (s *FriendService) Respond(responderID, initiatorID, action string) error {
	responderID = strings.TrimSpace(responderID)
	initiatorID = strings.TrimSpace(initiatorID)
	action = strings.TrimSpace(action)
	if responderID == "" || initiatorID == "" || action == "" {
		return NewFriendError(CodeParamInvalid, "target_user_id and action are required")
	}
	if action != "accept" && action != "reject" {
		return NewFriendError(CodeParamInvalid, "invalid action")
	}
	if responderID == initiatorID {
		return NewFriendError(CodeFriendNotFound, "friend request not found")
	}

	now := time.Now()
	newStatus := "accepted"
	if action == "reject" {
		newStatus = "rejected"
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		var rows []store.Friendship
		if err := tx.Where(
			"(user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?)",
			responderID, initiatorID, initiatorID, responderID,
		).Where("deleted_at IS NULL").Find(&rows).Error; err != nil {
			return err
		}
		found := false
		for _, r := range rows {
			if r.Status == "pending" && r.InitiatorID == initiatorID {
				found = true
				break
			}
		}
		if !found {
			return NewFriendError(CodeFriendNotFound, "friend request not found")
		}
		if err := tx.Model(&store.Friendship{}).
			Where(
				"(user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?)",
				responderID, initiatorID, initiatorID, responderID,
			).Where("deleted_at IS NULL").
			Updates(map[string]interface{}{
				"status":     newStatus,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		var fe *FriendError
		if errors.As(err, &fe) {
			return err
		}
		return NewFriendError(CodeInternal, "failed to respond")
	}

	s.invalidatePair(responderID, initiatorID)
	if newStatus == "accepted" {
		var responder store.User
		name := ""
		if err := s.db.Where("id = ?", responderID).First(&responder).Error; err == nil {
			name = responder.Username
		}
		s.emitAccept(responderID, name, initiatorID)
	}
	return nil
}

// Delete soft-deletes BOTH directions of the friendship.
func (s *FriendService) Delete(userID, friendID string) error {
	userID = strings.TrimSpace(userID)
	friendID = strings.TrimSpace(friendID)
	if userID == "" || friendID == "" {
		return NewFriendError(CodeParamInvalid, "friend_id is required")
	}
	if userID == friendID {
		return NewFriendError(CodeParamInvalid, "invalid friend_id")
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		var cnt int64
		if err := tx.Model(&store.Friendship{}).
			Where(
				"(user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?)",
				userID, friendID, friendID, userID,
			).Where("deleted_at IS NULL").Count(&cnt).Error; err != nil {
			return err
		}
		if cnt == 0 {
			return NewFriendError(CodeFriendNotFound, "friend not found")
		}
		if err := tx.Where(
			"(user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?)",
			userID, friendID, friendID, userID,
		).Delete(&store.Friendship{}).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		var fe *FriendError
		if errors.As(err, &fe) {
			return err
		}
		return NewFriendError(CodeInternal, "failed to delete friend")
	}
	s.invalidatePair(userID, friendID)
	return nil
}
