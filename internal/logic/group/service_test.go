package group

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"golang-im-neo-system/internal/store"
)

// ---------- helpers ----------

func newGroupTestService(t *testing.T) (*GroupService, *gorm.DB) {
	t.Helper()
	db, err := store.NewDB(store.DBConfig{Path: filepath.Join(t.TempDir(), "t.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return NewGroupService(db, nil, nil), db
}

func createGroupUser(t *testing.T, db *gorm.DB, id, username string) {
	t.Helper()
	if err := db.Create(&store.User{
		ID: id, Username: username, PasswordHash: "hash",
		Role: "user", Status: 1, TokenVersion: 1,
	}).Error; err != nil {
		t.Fatalf("create user %q: %v", username, err)
	}
}

func seedGroupUsers(t *testing.T, db *gorm.DB) {
	t.Helper()
	createGroupUser(t, db, "u_alice", "alice")
	createGroupUser(t, db, "u_bob", "bob")
	createGroupUser(t, db, "u_carol", "carol")
	createGroupUser(t, db, "u_dave", "dave")
}

func mustCreateGroup(t *testing.T, svc *GroupService, owner, name string) *GroupView {
	t.Helper()
	v, err := svc.CreateGroup(owner, name)
	if err != nil {
		t.Fatalf("CreateGroup(%q,%q): %v", owner, name, err)
	}
	if v.GroupID == "" {
		t.Fatal("CreateGroup returned empty group_id")
	}
	return v
}

func strptr(s string) *string { return &s }

// ---------- tests ----------

func TestCreateGroupOwnerAndConversation(t *testing.T) {
	svc, db := newGroupTestService(t)
	seedGroupUsers(t, db)

	v := mustCreateGroup(t, svc, "u_alice", "gophers")
	if v.Name != "gophers" || v.OwnerID != "u_alice" {
		t.Fatalf("view = %+v", v)
	}
	if v.MaxMembers != DefaultMaxMembers || v.MemberCount != 1 {
		t.Fatalf("view defaults wrong: %+v", v)
	}
	// Owner row.
	m, err := svc.GetMember(v.GroupID, "u_alice")
	if err != nil {
		t.Fatalf("GetMember owner: %v", err)
	}
	if m.Role != RoleOwner || m.Username != "alice" || m.JoinedVia != ViaCreate {
		t.Fatalf("owner member = %+v", m)
	}
	// Conversation row exists with groupchat type.
	var conv store.Conversation
	if err := db.Where("cov_id = ?", BuildCovID(v.GroupID)).First(&conv).Error; err != nil {
		t.Fatalf("conversation missing: %v", err)
	}
	if conv.ChatType != "groupchat" {
		t.Fatalf("chat_type = %q, want groupchat", conv.ChatType)
	}
	if BuildCovID("abc") != "cov:grp:abc" {
		t.Fatalf("BuildCovID wrong: %q", BuildCovID("abc"))
	}
	// Param + user errors.
	if _, err := svc.CreateGroup("u_alice", "  "); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("empty name: got %v, want 10001", err)
	}
	if _, err := svc.CreateGroup("u_ghost", "x"); CodeOf(err) != CodeGroupUserMissing {
		t.Fatalf("unknown owner: got %v, want 40009", err)
	}
	if _, err := svc.GetGroup("nope"); CodeOf(err) != CodeGroupNotFound {
		t.Fatalf("get missing: got %v, want 40001", err)
	}
}

func TestInviteJoinLeaveRevive(t *testing.T) {
	svc, db := newGroupTestService(t)
	seedGroupUsers(t, db)
	g := mustCreateGroup(t, svc, "u_alice", "g1")

	invited, err := svc.Invite("u_alice", g.GroupID, []string{"u_bob", "u_carol"})
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if len(invited) != 2 {
		t.Fatalf("invited = %v, want 2", invited)
	}
	// Already-active skipped, not an error.
	invited, err = svc.Invite("u_alice", g.GroupID, []string{"u_bob", "u_dave"})
	if err != nil {
		t.Fatalf("re-invite: %v", err)
	}
	if len(invited) != 1 || invited[0] != "u_dave" {
		t.Fatalf("re-invite invited = %v, want [u_dave]", invited)
	}
	// Invite by non-member -> 40002; unknown target -> 40009.
	if _, err := svc.Invite("u_ghost", g.GroupID, []string{"u_bob"}); CodeOf(err) != CodeGroupNotMember {
		t.Fatalf("non-member invite: got %v, want 40002", err)
	}
	if _, err := svc.Invite("u_alice", g.GroupID, []string{"u_ghost"}); CodeOf(err) != CodeGroupUserMissing {
		t.Fatalf("unknown target: got %v, want 40009", err)
	}

	members, err := svc.ListMembers(g.GroupID)
	if err != nil || len(members) != 4 {
		t.Fatalf("members = %+v err=%v, want 4", members, err)
	}

	// Leave + re-join revives with joined_via=join.
	if err := svc.Leave("u_bob", g.GroupID); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if _, err := svc.GetMember(g.GroupID, "u_bob"); CodeOf(err) != CodeGroupNotMember {
		t.Fatalf("left member visible: %v", err)
	}
	if err := svc.Join("u_bob", g.GroupID); err != nil {
		t.Fatalf("re-join: %v", err)
	}
	bob, err := svc.GetMember(g.GroupID, "u_bob")
	if err != nil {
		t.Fatalf("GetMember after rejoin: %v", err)
	}
	if bob.JoinedVia != ViaJoin || bob.Role != RoleMember {
		t.Fatalf("revived member = %+v, want role=member via=join", bob)
	}
	// No duplicate rows: unscoped count for bob must be 1.
	var n int64
	if err := db.Unscoped().Model(&store.GroupMember{}).
		Where("group_id = ? AND user_id = ?", g.GroupID, "u_bob").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("bob rows = %d, want 1 (revive, not insert)", n)
	}
	// Double join -> 40007.
	if err := svc.Join("u_bob", g.GroupID); CodeOf(err) != CodeGroupAlready {
		t.Fatalf("double join: got %v, want 40007", err)
	}
	// Leave non-member -> 40002; join missing group -> 40001.
	if err := svc.Leave("u_ghost", g.GroupID); CodeOf(err) != CodeGroupNotMember {
		t.Fatalf("leave non-member: got %v, want 40002", err)
	}
	if err := svc.Join("u_bob", "nope"); CodeOf(err) != CodeGroupNotFound {
		t.Fatalf("join missing: got %v, want 40001", err)
	}
}

func TestLeaveOwnerBlocked(t *testing.T) {
	svc, db := newGroupTestService(t)
	seedGroupUsers(t, db)
	g := mustCreateGroup(t, svc, "u_alice", "g1")

	if err := svc.Leave("u_alice", g.GroupID); CodeOf(err) != CodeGroupOwnerLeave {
		t.Fatalf("owner leave: got %v, want 40008", err)
	}
	// Owner still a member.
	if _, err := svc.GetMember(g.GroupID, "u_alice"); err != nil {
		t.Fatalf("owner gone after blocked leave: %v", err)
	}
}

func TestKickHierarchy(t *testing.T) {
	svc, db := newGroupTestService(t)
	seedGroupUsers(t, db)
	g := mustCreateGroup(t, svc, "u_alice", "g1")
	if _, err := svc.Invite("u_alice", g.GroupID, []string{"u_bob", "u_carol", "u_dave"}); err != nil {
		t.Fatal(err)
	}
	// Promote bob to admin.
	if err := svc.SetRole("u_alice", g.GroupID, "u_bob", RoleAdmin); err != nil {
		t.Fatalf("promote bob: %v", err)
	}
	// Admin kicks plain member: ok.
	if err := svc.Kick("u_bob", g.GroupID, "u_carol"); err != nil {
		t.Fatalf("admin kick member: %v", err)
	}
	// Admin kicks owner -> 40003.
	if err := svc.Kick("u_bob", g.GroupID, "u_alice"); CodeOf(err) != CodeGroupPermission {
		t.Fatalf("admin kick owner: got %v, want 40003", err)
	}
	// Promote dave via owner, then admin kicks admin -> 40003.
	if err := svc.SetRole("u_alice", g.GroupID, "u_dave", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := svc.Kick("u_bob", g.GroupID, "u_dave"); CodeOf(err) != CodeGroupPermission {
		t.Fatalf("admin kick admin: got %v, want 40003", err)
	}
	// Plain member kicks -> 40003. (carol was kicked; rejoin as member first.)
	if err := svc.Join("u_carol", g.GroupID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Kick("u_carol", g.GroupID, "u_dave"); CodeOf(err) != CodeGroupPermission {
		t.Fatalf("member kick: got %v, want 40003", err)
	}
	// Owner kicks admin: ok.
	if err := svc.Kick("u_alice", g.GroupID, "u_dave"); err != nil {
		t.Fatalf("owner kick admin: %v", err)
	}
	// Kick non-member target -> 40002.
	if err := svc.Kick("u_alice", g.GroupID, "u_dave"); CodeOf(err) != CodeGroupNotMember {
		t.Fatalf("kick non-member: got %v, want 40002", err)
	}
}

func TestSetRoleTransferAtomic(t *testing.T) {
	svc, db := newGroupTestService(t)
	seedGroupUsers(t, db)
	g := mustCreateGroup(t, svc, "u_alice", "g1")
	if _, err := svc.Invite("u_alice", g.GroupID, []string{"u_bob"}); err != nil {
		t.Fatal(err)
	}

	if err := svc.SetRole("u_alice", g.GroupID, "u_bob", "superadmin"); CodeOf(err) != CodeGroupRoleInvalid {
		t.Fatalf("bad role: got %v, want 40006", err)
	}
	if err := svc.SetRole("u_bob", g.GroupID, "u_bob", RoleAdmin); CodeOf(err) != CodeGroupPermission {
		t.Fatalf("non-owner setrole: got %v, want 40003", err)
	}
	// Transfer ownership alice -> bob.
	if err := svc.SetRole("u_alice", g.GroupID, "u_bob", RoleOwner); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	bob, _ := svc.GetMember(g.GroupID, "u_bob")
	alice, _ := svc.GetMember(g.GroupID, "u_alice")
	if bob.Role != RoleOwner || alice.Role != RoleAdmin {
		t.Fatalf("roles after transfer: alice=%q bob=%q", alice.Role, bob.Role)
	}
	var grow store.Group
	if err := db.Where("id = ?", g.GroupID).First(&grow).Error; err != nil {
		t.Fatal(err)
	}
	if grow.OwnerID != "u_bob" {
		t.Fatalf("owner_id = %q, want u_bob", grow.OwnerID)
	}
	// Old owner can no longer transfer; new owner can.
	if err := svc.SetRole("u_alice", g.GroupID, "u_alice", RoleOwner); CodeOf(err) != CodeGroupPermission {
		t.Fatalf("ex-owner transfer: got %v, want 40003", err)
	}
	if err := svc.SetRole("u_bob", g.GroupID, "u_alice", RoleOwner); err != nil {
		t.Fatalf("transfer back: %v", err)
	}
	// Self-demote is rejected.
	if err := svc.SetRole("u_alice", g.GroupID, "u_alice", RoleMember); CodeOf(err) != CodeGroupPermission {
		t.Fatalf("self demote: got %v, want 40003", err)
	}
}

func TestMutePermissions(t *testing.T) {
	svc, db := newGroupTestService(t)
	seedGroupUsers(t, db)
	g := mustCreateGroup(t, svc, "u_alice", "g1")
	if _, err := svc.Invite("u_alice", g.GroupID, []string{"u_bob", "u_carol"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRole("u_alice", g.GroupID, "u_bob", RoleAdmin); err != nil {
		t.Fatal(err)
	}

	// Owner mutes member.
	if err := svc.SetMuted("u_alice", g.GroupID, "u_carol", true); err != nil {
		t.Fatalf("mute: %v", err)
	}
	carol, _ := svc.GetMember(g.GroupID, "u_carol")
	if !carol.Muted {
		t.Fatal("carol should be muted")
	}
	var row store.GroupMember
	if err := db.Where("group_id = ? AND user_id = ?", g.GroupID, "u_carol").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.MutedBy != "u_alice" || row.MutedAt == 0 {
		t.Fatalf("mute audit wrong: by=%q at=%d", row.MutedBy, row.MutedAt)
	}
	muted, err := IsMuted(db, g.GroupID, "u_carol")
	if err != nil || !muted {
		t.Fatalf("IsMuted = %v,%v, want true,nil", muted, err)
	}
	// Unmute clears audit fields.
	if err := svc.SetMuted("u_alice", g.GroupID, "u_carol", false); err != nil {
		t.Fatalf("unmute: %v", err)
	}
	if err := db.Where("group_id = ? AND user_id = ?", g.GroupID, "u_carol").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Muted || row.MutedBy != "" || row.MutedAt != 0 {
		t.Fatalf("unmute did not clear: %+v", row)
	}
	// Admin cannot mute owner or other admins; can mute members.
	if err := svc.SetMuted("u_bob", g.GroupID, "u_alice", true); CodeOf(err) != CodeGroupPermission {
		t.Fatalf("admin mute owner: got %v, want 40003", err)
	}
	if err := svc.SetRole("u_alice", g.GroupID, "u_carol", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetMuted("u_bob", g.GroupID, "u_carol", true); CodeOf(err) != CodeGroupPermission {
		t.Fatalf("admin mute admin: got %v, want 40003", err)
	}
	if err := svc.SetRole("u_alice", g.GroupID, "u_carol", RoleMember); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetMuted("u_bob", g.GroupID, "u_carol", true); err != nil {
		t.Fatalf("admin mute member: %v", err)
	}
	// Plain member cannot mute.
	if err := svc.SetMuted("u_carol", g.GroupID, "u_bob", true); CodeOf(err) != CodeGroupPermission {
		t.Fatalf("member mute: got %v, want 40003", err)
	}
	// Muting a non-member -> 40002.
	if err := svc.SetMuted("u_alice", g.GroupID, "u_ghost", true); CodeOf(err) != CodeGroupNotMember {
		t.Fatalf("mute non-member: got %v, want 40002", err)
	}
}

func TestDismissBlocksJoin(t *testing.T) {
	svc, db := newGroupTestService(t)
	seedGroupUsers(t, db)
	g := mustCreateGroup(t, svc, "u_alice", "g1")
	if _, err := svc.Invite("u_alice", g.GroupID, []string{"u_bob"}); err != nil {
		t.Fatal(err)
	}

	if err := svc.Dismiss("u_bob", g.GroupID); CodeOf(err) != CodeGroupPermission {
		t.Fatalf("non-owner dismiss: got %v, want 40003", err)
	}
	if err := svc.Dismiss("u_alice", g.GroupID); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	var grow store.Group
	if err := db.Unscoped().Where("id = ?", g.GroupID).First(&grow).Error; err != nil {
		t.Fatal(err)
	}
	if grow.Status != 0 {
		t.Fatalf("status = %d, want 0", grow.Status)
	}
	var n int64
	if err := db.Model(&store.GroupMember{}).Where("group_id = ?", g.GroupID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("live members after dismiss = %d, want 0", n)
	}
	if err := svc.Join("u_carol", g.GroupID); CodeOf(err) != CodeGroupNotFound {
		t.Fatalf("join dismissed: got %v, want 40001", err)
	}
	mine, err := svc.ListMyGroups("u_alice")
	if err != nil || len(mine) != 0 {
		t.Fatalf("my groups after dismiss = %+v err=%v, want empty", mine, err)
	}
	// Owner dismiss is idempotent.
	if err := svc.Dismiss("u_alice", g.GroupID); err != nil {
		t.Fatalf("second dismiss: %v", err)
	}
}

func TestMaxMembers(t *testing.T) {
	svc, db := newGroupTestService(t)
	seedGroupUsers(t, db)
	g := mustCreateGroup(t, svc, "u_alice", "g1")
	if err := db.Model(&store.Group{}).Where("id = ?", g.GroupID).Update("max_members", 2).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invite("u_alice", g.GroupID, []string{"u_bob", "u_carol"}); CodeOf(err) != CodeGroupFull {
		t.Fatalf("invite over cap: got %v, want 40004", err)
	}
	if _, err := svc.Invite("u_alice", g.GroupID, []string{"u_bob"}); err != nil {
		t.Fatalf("invite within cap: %v", err)
	}
	if err := svc.Join("u_carol", g.GroupID); CodeOf(err) != CodeGroupFull {
		t.Fatalf("join over cap: got %v, want 40004", err)
	}
}

func TestUpdateGroupInfo(t *testing.T) {
	svc, db := newGroupTestService(t)
	seedGroupUsers(t, db)
	g := mustCreateGroup(t, svc, "u_alice", "g1")
	if _, err := svc.Invite("u_alice", g.GroupID, []string{"u_bob", "u_carol"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRole("u_alice", g.GroupID, "u_bob", RoleAdmin); err != nil {
		t.Fatal(err)
	}

	if err := svc.UpdateGroupInfo("u_bob", g.GroupID, strptr("new-name"), strptr("hello"), strptr("http://x/a.png")); err != nil {
		t.Fatalf("admin update: %v", err)
	}
	got, err := svc.GetGroup(g.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "new-name" || got.Announcement != "hello" || got.AvatarURL != "http://x/a.png" {
		t.Fatalf("updated view = %+v", got)
	}
	// Partial update leaves other fields alone.
	if err := svc.UpdateGroupInfo("u_alice", g.GroupID, nil, strptr("only-ann"), nil); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.GetGroup(g.GroupID)
	if got.Name != "new-name" || got.Announcement != "only-ann" || got.AvatarURL != "http://x/a.png" {
		t.Fatalf("partial update view = %+v", got)
	}
	// Member cannot update; empty name rejected.
	if err := svc.UpdateGroupInfo("u_carol", g.GroupID, strptr("x"), nil, nil); CodeOf(err) != CodeGroupPermission {
		t.Fatalf("member update: got %v, want 40003", err)
	}
	if err := svc.UpdateGroupInfo("u_alice", g.GroupID, strptr("  "), nil, nil); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("empty name: got %v, want 10001", err)
	}
}

func TestListMyGroups(t *testing.T) {
	svc, db := newGroupTestService(t)
	seedGroupUsers(t, db)
	g1 := mustCreateGroup(t, svc, "u_alice", "g1")
	mustCreateGroup(t, svc, "u_bob", "g2")
	// bob joins g1 via invite of alice.
	if _, err := svc.Invite("u_alice", g1.GroupID, []string{"u_bob"}); err != nil {
		t.Fatal(err)
	}
	mine, err := svc.ListMyGroups("u_bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 2 {
		t.Fatalf("bob groups = %+v, want 2", mine)
	}
	byID := map[string]GroupView{}
	for _, v := range mine {
		byID[v.GroupID] = v
	}
	if byID[g1.GroupID].MemberCount != 2 {
		t.Fatalf("g1 member_count = %d, want 2", byID[g1.GroupID].MemberCount)
	}
	carol, err := svc.ListMyGroups("u_carol")
	if err != nil || len(carol) != 0 {
		t.Fatalf("carol groups = %+v err=%v, want empty", carol, err)
	}
}

func TestHelpers(t *testing.T) {
	svc, db := newGroupTestService(t)
	seedGroupUsers(t, db)
	g := mustCreateGroup(t, svc, "u_alice", "g1")
	if _, err := svc.Invite("u_alice", g.GroupID, []string{"u_bob", "u_carol"}); err != nil {
		t.Fatal(err)
	}

	ok, err := IsMember(db, g.GroupID, "u_alice")
	if err != nil || !ok {
		t.Fatalf("IsMember owner = %v,%v", ok, err)
	}
	ok, err = IsMember(db, g.GroupID, "u_dave")
	if err != nil || ok {
		t.Fatalf("IsMember outsider = %v,%v, want false", ok, err)
	}
	ok, err = IsMember(db, "nope", "u_alice")
	if err != nil || ok {
		t.Fatalf("IsMember missing group = %v,%v, want false", ok, err)
	}
	muted, err := IsMuted(db, g.GroupID, "u_bob")
	if err != nil || muted {
		t.Fatalf("IsMuted unmuted = %v,%v", muted, err)
	}
	if err := svc.SetMuted("u_alice", g.GroupID, "u_bob", true); err != nil {
		t.Fatal(err)
	}
	muted, err = IsMuted(db, g.GroupID, "u_bob")
	if err != nil || !muted {
		t.Fatalf("IsMuted muted = %v,%v, want true", muted, err)
	}
	muted, err = IsMuted(db, g.GroupID, "u_dave")
	if err != nil || muted {
		t.Fatalf("IsMuted outsider = %v,%v, want false", muted, err)
	}
	ids, err := ListActiveMemberIDs(db, g.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"u_alice": true, "u_bob": true, "u_carol": true}
	if len(ids) != 3 {
		t.Fatalf("member ids = %v, want 3", ids)
	}
	for _, id := range ids {
		if !want[id] {
			t.Fatalf("unexpected member %q in %v", id, ids)
		}
	}
	// Leave shrinks fan-out; dismiss empties it.
	if err := svc.Leave("u_carol", g.GroupID); err != nil {
		t.Fatal(err)
	}
	ids, _ = ListActiveMemberIDs(db, g.GroupID)
	if len(ids) != 2 {
		t.Fatalf("after leave ids = %v, want 2", ids)
	}
	if ok, _ := IsMember(db, g.GroupID, "u_carol"); ok {
		t.Fatal("IsMember should be false after leave")
	}
	if err := svc.Dismiss("u_alice", g.GroupID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := IsMember(db, g.GroupID, "u_alice"); ok {
		t.Fatal("IsMember should be false after dismiss")
	}
	ids, _ = ListActiveMemberIDs(db, g.GroupID)
	if len(ids) != 0 {
		t.Fatalf("after dismiss ids = %v, want empty", ids)
	}
	ids, err = ListActiveMemberIDs(db, "nope")
	if err != nil || len(ids) != 0 {
		t.Fatalf("missing group ids = %v err=%v, want empty", ids, err)
	}
	_ = time.Now
}

// ---------- invalidator wiring (F2) ----------

type fakeGroupInvalidator struct {
	mu    sync.Mutex
	calls []groupMemberKey
}

type groupMemberKey struct {
	groupID string
	userID  string
}

func (f *fakeGroupInvalidator) InvalidateGroupMember(groupID, userID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, groupMemberKey{groupID: groupID, userID: userID})
}

func (f *fakeGroupInvalidator) has(groupID, userID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.groupID == groupID && c.userID == userID {
			return true
		}
	}
	return false
}

func (f *fakeGroupInvalidator) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

func newGroupTestServiceWithInvalidator(t *testing.T, iv Invalidator) (*GroupService, *gorm.DB) {
	t.Helper()
	db, err := store.NewDB(store.DBConfig{Path: filepath.Join(t.TempDir(), "t.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return NewGroupService(db, iv, nil), db
}

func TestInvalidatorWiring(t *testing.T) {
	iv := &fakeGroupInvalidator{}
	svc, db := newGroupTestServiceWithInvalidator(t, iv)
	seedGroupUsers(t, db)
	g := mustCreateGroup(t, svc, "u_alice", "g1")
	gid := g.GroupID

	// Create alone invalidates nothing.
	if n := len(iv.calls); n != 0 {
		t.Fatalf("create invalidations = %d, want 0", n)
	}

	// Invite invalidates each newly added member.
	iv.reset()
	if _, err := svc.Invite("u_alice", gid, []string{"u_bob", "u_carol"}); err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if !iv.has(gid, "u_bob") || !iv.has(gid, "u_carol") {
		t.Fatalf("invite invalidations = %+v, want bob+carol", iv.calls)
	}

	// Join invalidates the joining user.
	iv.reset()
	if err := svc.Join("u_dave", gid); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if !iv.has(gid, "u_dave") {
		t.Fatalf("join invalidations = %+v, want dave", iv.calls)
	}

	// SetMuted invalidates the mute target.
	iv.reset()
	if err := svc.SetMuted("u_alice", gid, "u_carol", true); err != nil {
		t.Fatalf("SetMuted: %v", err)
	}
	if !iv.has(gid, "u_carol") {
		t.Fatalf("mute invalidations = %+v, want carol", iv.calls)
	}

	// Kick invalidates the kicked user.
	iv.reset()
	if err := svc.Kick("u_alice", gid, "u_bob"); err != nil {
		t.Fatalf("Kick: %v", err)
	}
	if !iv.has(gid, "u_bob") {
		t.Fatalf("kick invalidations = %+v, want bob", iv.calls)
	}

	// Leave invalidates the leaving user.
	iv.reset()
	if err := svc.Leave("u_carol", gid); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if !iv.has(gid, "u_carol") {
		t.Fatalf("leave invalidations = %+v, want carol", iv.calls)
	}

	// Failed operations invalidate nothing.
	iv.reset()
	if err := svc.Kick("u_alice", gid, "u_bob"); CodeOf(err) != CodeGroupNotMember {
		t.Fatalf("kick non-member: got %v, want 40002", err)
	}
	if len(iv.calls) != 0 {
		t.Fatalf("failed kick invalidations = %+v, want none", iv.calls)
	}

	// Dismiss invalidates every remaining member (owner alice + dave).
	iv.reset()
	if err := svc.Dismiss("u_alice", gid); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if !iv.has(gid, "u_alice") || !iv.has(gid, "u_dave") {
		t.Fatalf("dismiss invalidations = %+v, want alice+dave", iv.calls)
	}
}
