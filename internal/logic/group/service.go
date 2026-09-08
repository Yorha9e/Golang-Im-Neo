// Package group implements the group-chat domain business logic (M1).
//
// This file holds the pure business logic: no gin imports here. The HTTP
// adapter lives in http.go; router-facing read helpers live in helpers.go.
//
// Model: store.Group (groups) + store.GroupMember (group_members, soft-delete
// with partial unique index so re-join after leave revives cleanly). Every
// group owns a conversation row cov:grp:<group_id> (chatType groupchat).
package group

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/store"
)

// Group error block (stage 4, M1).
const (
	CodeSuccess          = 0
	CodeParamInvalid     = 10001 // ERR_PARAM_INVALID
	CodeUnauthorized     = 10002 // ERR_UNAUTHORIZED
	CodeForbidden        = 10003 // ERR_FORBIDDEN
	CodeGroupNotFound    = 40001 // ERR_GROUP_NOT_FOUND
	CodeGroupNotMember   = 40002 // ERR_GROUP_NOT_MEMBER
	CodeGroupPermission  = 40003 // ERR_GROUP_PERMISSION (not owner/admin)
	CodeGroupFull        = 40004 // ERR_GROUP_FULL
	CodeGroupMuted       = 40005 // ERR_GROUP_MUTED
	CodeGroupRoleInvalid = 40006 // ERR_GROUP_ROLE_INVALID
	CodeGroupAlready     = 40007 // ERR_GROUP_ALREADY_MEMBER
	CodeGroupOwnerLeave  = 40008 // ERR_GROUP_OWNER_LEAVE (owner must transfer first)
	CodeGroupUserMissing = 40009 // ERR_GROUP_USER_NOT_FOUND
	CodeInternal         = 50001 // ERR_INTERNAL_SERVER
)

// GroupError is a typed business error carrying a stage-4 code.
type GroupError struct {
	Code int
	Msg  string
}

func (e *GroupError) Error() string { return e.Msg }

// NewGroupError builds a typed group-domain error.
func NewGroupError(code int, msg string) *GroupError { return &GroupError{Code: code, Msg: msg} }

// CodeOf unwraps the business code from err; unknown errors map to 50001.
func CodeOf(err error) int {
	var ge *GroupError
	if errors.As(err, &ge) {
		return ge.Code
	}
	return CodeInternal
}

// HTTPStatusOf maps a business code to a proper HTTP status.
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
	case CodeGroupNotFound:
		return 404
	case CodeGroupNotMember:
		return 403
	case CodeGroupPermission:
		return 403
	case CodeGroupFull:
		return 409
	case CodeGroupMuted:
		return 403
	case CodeGroupRoleInvalid:
		return 400
	case CodeGroupAlready:
		return 409
	case CodeGroupOwnerLeave:
		return 400
	case CodeGroupUserMissing:
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

// Roles and join sources.
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"

	ViaCreate = "create"
	ViaInvite = "invite"
	ViaJoin   = "join"
)

// DefaultMaxMembers applies to newly created groups.
const DefaultMaxMembers = 500

// GroupService is the pure group business logic.
type GroupService struct {
	db          *gorm.DB
	invalidator Invalidator
	logger      *zap.Logger
}

// Invalidator drops a cached group-membership verdict.
// Satisfied by *router.Router.
type Invalidator interface {
	InvalidateGroupMember(groupID, userID string)
}

// NewGroupService builds the service.
func NewGroupService(db *gorm.DB, invalidator Invalidator, logger *zap.Logger) *GroupService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &GroupService{db: db, invalidator: invalidator, logger: logger}
}

// GroupView is the JSON view of a group.
type GroupView struct {
	GroupID      string    `json:"group_id"`
	Name         string    `json:"name"`
	AvatarURL    string    `json:"avatar_url"`
	Announcement string    `json:"announcement"`
	OwnerID      string    `json:"owner_id"`
	MaxMembers   int       `json:"max_members"`
	MemberCount  int64     `json:"member_count"`
	CreatedAt    time.Time `json:"created_at"`
}

// MemberView is the JSON view of a group member (joined with users.username).
type MemberView struct {
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Muted     bool      `json:"muted"`
	JoinedVia string    `json:"joined_via"`
	JoinedAt  time.Time `json:"joined_at"`
}

// ---------- internal helpers (tx-scoped: db may be *gorm.DB or a tx) ----------

// loadGroup fetches the group row (excluding soft-deleted).
func loadGroup(db *gorm.DB, groupID string) (*store.Group, error) {
	var g store.Group
	if err := db.Where("id = ?", groupID).First(&g).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, NewGroupError(CodeGroupNotFound, "group not found")
		}
		return nil, NewGroupError(CodeInternal, "failed to load group")
	}
	return &g, nil
}

// loadActiveGroup fetches the group row and requires status=1.
func loadActiveGroup(db *gorm.DB, groupID string) (*store.Group, error) {
	g, err := loadGroup(db, groupID)
	if err != nil {
		return nil, err
	}
	if g.Status != 1 {
		return nil, NewGroupError(CodeGroupNotFound, "group not found")
	}
	return g, nil
}

// loadMember fetches the ACTIVE member row, or (nil, nil) when not a member.
func loadMember(db *gorm.DB, groupID, userID string) (*store.GroupMember, error) {
	var m store.GroupMember
	if err := db.Where("group_id = ? AND user_id = ?", groupID, userID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, NewGroupError(CodeInternal, "failed to load membership")
	}
	return &m, nil
}

// requireMember returns the active member row or 40002.
func requireMember(db *gorm.DB, groupID, userID string) (*store.GroupMember, error) {
	m, err := loadMember(db, groupID, userID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, NewGroupError(CodeGroupNotMember, "not a group member")
	}
	return m, nil
}

// requireAdmin returns the member row when it carries owner/admin, else 40003.
func requireAdmin(db *gorm.DB, groupID, userID string) (*store.GroupMember, error) {
	m, err := loadMember(db, groupID, userID)
	if err != nil {
		return nil, err
	}
	if m == nil || (m.Role != RoleOwner && m.Role != RoleAdmin) {
		return nil, NewGroupError(CodeGroupPermission, "owner or admin required")
	}
	return m, nil
}

// activeCount counts ACTIVE members of the group.
func activeCount(db *gorm.DB, groupID string) (int64, error) {
	var n int64
	if err := db.Model(&store.GroupMember{}).
		Where("group_id = ?", groupID).
		Count(&n).Error; err != nil {
		return 0, NewGroupError(CodeInternal, "failed to count members")
	}
	return n, nil
}

// ensureConversation creates the cov:grp:<id> groupchat row when absent.
// It runs inside the caller's transaction and never imports the session package.
func ensureConversation(tx *gorm.DB, groupID string) error {
	cov := BuildCovID(groupID)
	var c store.Conversation
	if err := tx.Where("cov_id = ?", cov).First(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c = store.Conversation{CovID: cov, ChatType: "groupchat"}
			if err := tx.Create(&c).Error; err != nil {
				return NewGroupError(CodeInternal, "failed to create conversation")
			}
			return nil
		}
		return NewGroupError(CodeInternal, "failed to load conversation")
	}
	return nil
}

// toGroupView builds the JSON view with a live active-member count.
func toGroupView(db *gorm.DB, g *store.Group) (*GroupView, error) {
	n, err := activeCount(db, g.ID)
	if err != nil {
		return nil, err
	}
	return &GroupView{
		GroupID:      g.ID,
		Name:         g.Name,
		AvatarURL:    g.AvatarURL,
		Announcement: g.Announcement,
		OwnerID:      g.OwnerID,
		MaxMembers:   g.MaxMembers,
		MemberCount:  n,
		CreatedAt:    g.CreatedAt,
	}, nil
}

// toMemberView joins the username for one member row.
func toMemberView(db *gorm.DB, m *store.GroupMember) (*MemberView, error) {
	var u store.User
	username := ""
	if err := db.Select("username").Where("id = ?", m.UserID).First(&u).Error; err == nil {
		username = u.Username
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewGroupError(CodeInternal, "failed to load user")
	}
	return &MemberView{
		UserID:    m.UserID,
		Username:  username,
		Role:      m.Role,
		Muted:     m.Muted,
		JoinedVia: m.JoinedVia,
		JoinedAt:  m.CreatedAt,
	}, nil
}

// userExists reports whether the users row exists (excluding soft-deleted).
func userExists(db *gorm.DB, userID string) (bool, error) {
	var n int64
	if err := db.Model(&store.User{}).Where("id = ?", userID).Count(&n).Error; err != nil {
		return false, NewGroupError(CodeInternal, "failed to load user")
	}
	return n > 0, nil
}

// ---------- lifecycle ----------

// CreateGroup creates a group with the owner row (role=owner, joined_via=create)
// plus the cov:grp:<id> conversation row in ONE transaction.
func (s *GroupService) CreateGroup(ownerID, name string) (*GroupView, error) {
	ownerID = strings.TrimSpace(ownerID)
	name = strings.TrimSpace(name)
	if ownerID == "" || name == "" {
		return nil, NewGroupError(CodeParamInvalid, "owner and name are required")
	}
	ok, err := userExists(s.db, ownerID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, NewGroupError(CodeGroupUserMissing, "user not found")
	}
	id := uuid.NewString()
	now := time.Now()
	g := &store.Group{
		ID:         id,
		Name:       name,
		OwnerID:    ownerID,
		MaxMembers: DefaultMaxMembers,
		Status:     1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(g).Error; err != nil {
			return err
		}
		m := &store.GroupMember{
			GroupID:   id,
			UserID:    ownerID,
			Role:      RoleOwner,
			JoinedVia: ViaCreate,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := tx.Create(m).Error; err != nil {
			return err
		}
		return ensureConversation(tx, id)
	}); err != nil {
		var ge *GroupError
		if errors.As(err, &ge) {
			return nil, err
		}
		return nil, NewGroupError(CodeInternal, "failed to create group")
	}
	return toGroupView(s.db, g)
}

// UpdateGroupInfo updates name/announcement/avatar (non-nil fields only).
// Owner or admin only.
func (s *GroupService) UpdateGroupInfo(actorID, groupID string, name, announcement, avatarURL *string) error {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(groupID) == "" {
		return NewGroupError(CodeParamInvalid, "actor and group are required")
	}
	if name != nil && strings.TrimSpace(*name) == "" {
		return NewGroupError(CodeParamInvalid, "name must not be empty")
	}
	g, err := loadActiveGroup(s.db, groupID)
	if err != nil {
		return err
	}
	if _, err := requireAdmin(s.db, groupID, actorID); err != nil {
		return err
	}
	updates := map[string]interface{}{"updated_at": time.Now()}
	if name != nil {
		updates["name"] = strings.TrimSpace(*name)
	}
	if announcement != nil {
		updates["announcement"] = *announcement
	}
	if avatarURL != nil {
		updates["avatar_url"] = *avatarURL
	}
	if err := s.db.Model(&store.Group{}).Where("id = ?", g.ID).Updates(updates).Error; err != nil {
		return NewGroupError(CodeInternal, "failed to update group")
	}
	return nil
}

// Invite adds target users as members (joined_via=invite). The actor must be
// an active member. Already-active members are skipped (not an error) and
// soft-deleted rows are revived with role reset to member. MaxMembers is
// enforced against the active count. All-or-nothing in one transaction.
func (s *GroupService) Invite(actorID, groupID string, targetUserIDs []string) ([]string, error) {
	invited := []string{}
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(groupID) == "" {
		return invited, NewGroupError(CodeParamInvalid, "actor and group are required")
	}
	g, err := loadActiveGroup(s.db, groupID)
	if err != nil {
		return invited, err
	}
	if _, err := requireMember(s.db, groupID, actorID); err != nil {
		return invited, err
	}
	// Dedupe + drop blanks.
	seen := map[string]bool{}
	targets := []string{}
	for _, t := range targetUserIDs {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		targets = append(targets, t)
	}
	if len(targets) == 0 {
		return invited, nil
	}
	// Every target must exist.
	var users []store.User
	if err := s.db.Select("id").Where("id IN ?", targets).Find(&users).Error; err != nil {
		return invited, NewGroupError(CodeInternal, "failed to load users")
	}
	have := map[string]bool{}
	for _, u := range users {
		have[u.ID] = true
	}
	for _, t := range targets {
		if !have[t] {
			return invited, NewGroupError(CodeGroupUserMissing, "user not found")
		}
	}
	// Skip already-active members.
	var live []store.GroupMember
	if err := s.db.Select("user_id").Where("group_id = ? AND user_id IN ?", groupID, targets).Find(&live).Error; err != nil {
		return invited, NewGroupError(CodeInternal, "failed to check membership")
	}
	liveSet := map[string]bool{}
	for _, m := range live {
		liveSet[m.UserID] = true
	}
	toAdd := []string{}
	for _, t := range targets {
		if !liveSet[t] {
			toAdd = append(toAdd, t)
		}
	}
	if len(toAdd) == 0 {
		return invited, nil
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		n, err := activeCount(tx, groupID)
		if err != nil {
			return err
		}
		if n+int64(len(toAdd)) > int64(g.MaxMembers) {
			return NewGroupError(CodeGroupFull, "group is full")
		}
		now := time.Now()
		for _, t := range toAdd {
			var old store.GroupMember
			err := tx.Unscoped().Where("group_id = ? AND user_id = ?", groupID, t).First(&old).Error
			if err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				if err := tx.Create(&store.GroupMember{
					GroupID:   groupID,
					UserID:    t,
					Role:      RoleMember,
					JoinedVia: ViaInvite,
					CreatedAt: now,
					UpdatedAt: now,
				}).Error; err != nil {
					return err
				}
				continue
			}
			// Revive: reset to a plain member invited anew.
			if err := tx.Unscoped().Model(&store.GroupMember{}).
				Where("group_id = ? AND user_id = ?", groupID, t).
				Updates(map[string]interface{}{
					"role":       RoleMember,
					"muted":      false,
					"muted_by":   "",
					"muted_at":   0,
					"joined_via": ViaInvite,
					"updated_at": now,
					"deleted_at": nil,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		var ge *GroupError
		if errors.As(err, &ge) {
			return invited, err
		}
		return invited, NewGroupError(CodeInternal, "failed to invite")
	}
	invited = append(invited, toAdd...)
	if s.invalidator != nil {
		for _, uid := range invited {
			s.invalidator.InvalidateGroupMember(groupID, uid)
		}
	}
	return invited, nil
}

// Join adds the user as a member (joined_via=join), reviving a soft-deleted
// row when the user left before.
func (s *GroupService) Join(userID, groupID string) error {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(groupID) == "" {
		return NewGroupError(CodeParamInvalid, "user and group are required")
	}
	g, err := loadActiveGroup(s.db, groupID)
	if err != nil {
		return err
	}
	ok, err := userExists(s.db, userID)
	if err != nil {
		return err
	}
	if !ok {
		return NewGroupError(CodeGroupUserMissing, "user not found")
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if m, err := loadMember(tx, groupID, userID); err != nil {
			return err
		} else if m != nil {
			return NewGroupError(CodeGroupAlready, "already a member")
		}
		n, err := activeCount(tx, groupID)
		if err != nil {
			return err
		}
		if n+1 > int64(g.MaxMembers) {
			return NewGroupError(CodeGroupFull, "group is full")
		}
		now := time.Now()
		var old store.GroupMember
		err = tx.Unscoped().Where("group_id = ? AND user_id = ?", groupID, userID).First(&old).Error
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			return tx.Create(&store.GroupMember{
				GroupID:   groupID,
				UserID:    userID,
				Role:      RoleMember,
				JoinedVia: ViaJoin,
				CreatedAt: now,
				UpdatedAt: now,
			}).Error
		}
		return tx.Unscoped().Model(&store.GroupMember{}).
			Where("group_id = ? AND user_id = ?", groupID, userID).
			Updates(map[string]interface{}{
				"role":       RoleMember,
				"muted":      false,
				"muted_by":   "",
				"muted_at":   0,
				"joined_via": ViaJoin,
				"updated_at": now,
				"deleted_at": nil,
			}).Error
	}); err != nil {
		var ge *GroupError
		if errors.As(err, &ge) {
			return err
		}
		return NewGroupError(CodeInternal, "failed to join group")
	}
	if s.invalidator != nil {
		s.invalidator.InvalidateGroupMember(groupID, userID)
	}
	return nil
}

// Leave soft-deletes the member row. The owner cannot leave (40008) and must
// transfer ownership first.
func (s *GroupService) Leave(userID, groupID string) error {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(groupID) == "" {
		return NewGroupError(CodeParamInvalid, "user and group are required")
	}
	if _, err := loadActiveGroup(s.db, groupID); err != nil {
		// Leaving a missing group reads as not-a-member when the group row is
		// gone entirely; a dismissed group also has no live rows.
		if CodeOf(err) == CodeGroupNotFound {
			if _, lerr := loadGroup(s.db, groupID); lerr == nil {
				return NewGroupError(CodeGroupNotMember, "not a group member")
			}
		}
		return err
	}
	m, err := requireMember(s.db, groupID, userID)
	if err != nil {
		return err
	}
	if m.Role == RoleOwner {
		return NewGroupError(CodeGroupOwnerLeave, "owner must transfer ownership first")
	}
	if err := s.db.Where("group_id = ? AND user_id = ?", groupID, userID).Delete(&store.GroupMember{}).Error; err != nil {
		return NewGroupError(CodeInternal, "failed to leave group")
	}
	if s.invalidator != nil {
		s.invalidator.InvalidateGroupMember(groupID, userID)
	}
	return nil
}

// Kick removes targetID. Actor must be owner/admin with hierarchy: the owner
// may kick anyone except another owner; an admin may kick plain members only.
func (s *GroupService) Kick(actorID, groupID, targetID string) error {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(groupID) == "" || strings.TrimSpace(targetID) == "" {
		return NewGroupError(CodeParamInvalid, "actor, group and target are required")
	}
	if _, err := loadActiveGroup(s.db, groupID); err != nil {
		return err
	}
	actor, err := requireAdmin(s.db, groupID, actorID)
	if err != nil {
		return err
	}
	target, err := requireMember(s.db, groupID, targetID)
	if err != nil {
		return err
	}
	if target.Role == RoleOwner {
		return NewGroupError(CodeGroupPermission, "cannot kick the owner")
	}
	if actor.Role == RoleAdmin && target.Role != RoleMember {
		return NewGroupError(CodeGroupPermission, "admin can only kick members")
	}
	if err := s.db.Where("group_id = ? AND user_id = ?", groupID, targetID).Delete(&store.GroupMember{}).Error; err != nil {
		return NewGroupError(CodeInternal, "failed to kick member")
	}
	if s.invalidator != nil {
		s.invalidator.InvalidateGroupMember(groupID, targetID)
	}
	return nil
}

// SetRole changes targetID's role. Only the owner may call it; role must be
// admin|member, except role=owner which performs an atomic ownership transfer
// (actor demoted to admin, target promoted, groups.owner_id swapped).
func (s *GroupService) SetRole(actorID, groupID, targetID, role string) error {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(groupID) == "" || strings.TrimSpace(targetID) == "" {
		return NewGroupError(CodeParamInvalid, "actor, group and target are required")
	}
	role = strings.TrimSpace(role)
	if role != RoleAdmin && role != RoleMember && role != RoleOwner {
		return NewGroupError(CodeGroupRoleInvalid, "invalid role")
	}
	g, err := loadActiveGroup(s.db, groupID)
	if err != nil {
		return err
	}
	if g.OwnerID != actorID {
		return NewGroupError(CodeGroupPermission, "owner required")
	}
	target, err := requireMember(s.db, groupID, targetID)
	if err != nil {
		return err
	}
	now := time.Now()
	if role == RoleOwner {
		if targetID == actorID {
			return nil // already the owner: idempotent no-op
		}
		if err := s.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&store.GroupMember{}).
				Where("group_id = ? AND user_id = ?", groupID, actorID).
				Updates(map[string]interface{}{"role": RoleAdmin, "updated_at": now}).Error; err != nil {
				return err
			}
			if err := tx.Model(&store.GroupMember{}).
				Where("group_id = ? AND user_id = ?", groupID, targetID).
				Updates(map[string]interface{}{"role": RoleOwner, "updated_at": now}).Error; err != nil {
				return err
			}
			return tx.Model(&store.Group{}).
				Where("id = ?", groupID).
				Updates(map[string]interface{}{"owner_id": targetID, "updated_at": now}).Error
		}); err != nil {
			return NewGroupError(CodeInternal, "failed to transfer ownership")
		}
		return nil
	}
	if targetID == actorID {
		return NewGroupError(CodeGroupPermission, "cannot change own role, transfer ownership first")
	}
	if target.Role == role {
		return nil
	}
	if err := s.db.Model(&store.GroupMember{}).
		Where("group_id = ? AND user_id = ?", groupID, targetID).
		Updates(map[string]interface{}{"role": role, "updated_at": now}).Error; err != nil {
		return NewGroupError(CodeInternal, "failed to set role")
	}
	return nil
}

// SetMuted mutes/unmutes targetID. Owner or admin only; an admin cannot mute
// the owner or other admins.
func (s *GroupService) SetMuted(actorID, groupID, targetID string, muted bool) error {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(groupID) == "" || strings.TrimSpace(targetID) == "" {
		return NewGroupError(CodeParamInvalid, "actor, group and target are required")
	}
	if _, err := loadActiveGroup(s.db, groupID); err != nil {
		return err
	}
	actor, err := requireAdmin(s.db, groupID, actorID)
	if err != nil {
		return err
	}
	target, err := requireMember(s.db, groupID, targetID)
	if err != nil {
		return err
	}
	if actor.Role == RoleAdmin && target.Role != RoleMember {
		return NewGroupError(CodeGroupPermission, "admin cannot mute owner or admins")
	}
	now := time.Now()
	updates := map[string]interface{}{"muted": muted, "updated_at": now}
	if muted {
		updates["muted_by"] = actorID
		updates["muted_at"] = now.UnixMilli()
	} else {
		updates["muted_by"] = ""
		updates["muted_at"] = 0
	}
	if err := s.db.Model(&store.GroupMember{}).
		Where("group_id = ? AND user_id = ?", groupID, targetID).
		Updates(updates).Error; err != nil {
		return NewGroupError(CodeInternal, "failed to set mute")
	}
	if s.invalidator != nil {
		s.invalidator.InvalidateGroupMember(groupID, targetID)
	}
	return nil
}

// Dismiss dissolves the group. Owner only (checked against groups.owner_id so
// the check stays valid); sets status=0 and soft-deletes all member rows in
// one transaction. Idempotent for the owner.
func (s *GroupService) Dismiss(actorID, groupID string) error {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(groupID) == "" {
		return NewGroupError(CodeParamInvalid, "actor and group are required")
	}
	g, err := loadGroup(s.db, groupID)
	if err != nil {
		return err
	}
	if g.OwnerID != actorID {
		return NewGroupError(CodeGroupPermission, "owner required")
	}
	if g.Status == 0 {
		return nil
	}
	// Snapshot active members so every cached membership verdict can be dropped
	// after the rows are gone.
	var memberIDs []string
	if s.invalidator != nil {
		var rows []store.GroupMember
		if err := s.db.Select("user_id").Where("group_id = ?", groupID).Find(&rows).Error; err != nil {
			return NewGroupError(CodeInternal, "failed to load members")
		}
		for _, m := range rows {
			memberIDs = append(memberIDs, m.UserID)
		}
	}
	now := time.Now()
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&store.Group{}).
			Where("id = ?", groupID).
			Updates(map[string]interface{}{"status": 0, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Where("group_id = ?", groupID).Delete(&store.GroupMember{}).Error
	}); err != nil {
		return NewGroupError(CodeInternal, "failed to dismiss group")
	}
	if s.invalidator != nil {
		for _, uid := range memberIDs {
			s.invalidator.InvalidateGroupMember(groupID, uid)
		}
	}
	return nil
}

// ---------- reads ----------

// GetGroup returns the group view (live active-member count included).
func (s *GroupService) GetGroup(groupID string) (*GroupView, error) {
	if strings.TrimSpace(groupID) == "" {
		return nil, NewGroupError(CodeParamInvalid, "group is required")
	}
	g, err := loadGroup(s.db, groupID)
	if err != nil {
		return nil, err
	}
	return toGroupView(s.db, g)
}

// ListMyGroups returns active groups the user actively belongs to.
func (s *GroupService) ListMyGroups(userID string) ([]GroupView, error) {
	out := []GroupView{}
	if strings.TrimSpace(userID) == "" {
		return nil, NewGroupError(CodeParamInvalid, "user is required")
	}
	type row struct {
		ID           string
		Name         string
		AvatarURL    string
		Announcement string
		OwnerID      string
		MaxMembers   int
		CreatedAt    time.Time
	}
	var rows []row
	if err := s.db.Table("group_members m").
		Select("g.id, g.name, g.avatar_url, g.announcement, g.owner_id, g.max_members, g.created_at").
		Joins("JOIN groups g ON g.id = m.group_id").
		Where("m.user_id = ? AND m.deleted_at IS NULL AND g.status = 1 AND g.deleted_at IS NULL", userID).
		Order("g.created_at ASC, g.id ASC").
		Scan(&rows).Error; err != nil {
		return nil, NewGroupError(CodeInternal, "failed to list groups")
	}
	for _, r := range rows {
		n, err := activeCount(s.db, r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, GroupView{
			GroupID:      r.ID,
			Name:         r.Name,
			AvatarURL:    r.AvatarURL,
			Announcement: r.Announcement,
			OwnerID:      r.OwnerID,
			MaxMembers:   r.MaxMembers,
			MemberCount:  n,
			CreatedAt:    r.CreatedAt,
		})
	}
	return out, nil
}

// ListMembers returns active members with usernames, in join order.
func (s *GroupService) ListMembers(groupID string) ([]MemberView, error) {
	out := []MemberView{}
	if strings.TrimSpace(groupID) == "" {
		return nil, NewGroupError(CodeParamInvalid, "group is required")
	}
	if _, err := loadGroup(s.db, groupID); err != nil {
		return nil, err
	}
	var members []store.GroupMember
	if err := s.db.Where("group_id = ?", groupID).Order("id ASC").Find(&members).Error; err != nil {
		return nil, NewGroupError(CodeInternal, "failed to list members")
	}
	for i := range members {
		v, err := toMemberView(s.db, &members[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// GetMember returns one active member or 40002 when not a member.
func (s *GroupService) GetMember(groupID, userID string) (*MemberView, error) {
	if strings.TrimSpace(groupID) == "" || strings.TrimSpace(userID) == "" {
		return nil, NewGroupError(CodeParamInvalid, "group and user are required")
	}
	if _, err := loadGroup(s.db, groupID); err != nil {
		return nil, err
	}
	m, err := requireMember(s.db, groupID, userID)
	if err != nil {
		return nil, err
	}
	return toMemberView(s.db, m)
}
