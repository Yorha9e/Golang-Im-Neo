package friend

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/logic/session"
	"golang-im-neo-system/internal/store"
	pb "golang-im-neo-system/proto"
)

// ---------- fakes (shared with http_test.go) ----------

type emittedCall struct {
	UserID string
	Raw    []byte
	Msg    *pb.WsMessage
}

type fakeEmitter struct {
	mu    sync.Mutex
	calls []emittedCall
}

func (f *fakeEmitter) SendToUser(userID string, msg []byte) bool {
	m := &pb.WsMessage{}
	_ = proto.Unmarshal(msg, m)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, emittedCall{UserID: userID, Raw: msg, Msg: m})
	return true
}

func (f *fakeEmitter) byType(t pb.MsgType) []emittedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []emittedCall
	for _, c := range f.calls {
		if c.Msg.GetType() == t {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeEmitter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type fakeInvalidator struct {
	mu   sync.Mutex
	keys []string
}

func (f *fakeInvalidator) Invalidate(covKey string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, covKey)
}

func (f *fakeInvalidator) has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range f.keys {
		if k == key {
			return true
		}
	}
	return false
}

func (f *fakeInvalidator) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.keys)
}

// ---------- helpers ----------

func newFriendTestService(t *testing.T) (*FriendService, *gorm.DB, *fakeEmitter, *fakeInvalidator) {
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
	em := &fakeEmitter{}
	iv := &fakeInvalidator{}
	svc := NewFriendService(db, em, iv, nil)
	return svc, db, em, iv
}

func createFriendUser(t *testing.T, db *gorm.DB, id, username string) {
	t.Helper()
	u := &store.User{
		ID:           id,
		Username:     username,
		PasswordHash: "hash",
		Role:         "user",
		Status:       1,
		TokenVersion: 1,
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("create user %q: %v", username, err)
	}
}

func mustApply(t *testing.T, svc *FriendService, applicantID, targetUsername, remark string) string {
	t.Helper()
	tid, err := svc.Apply(applicantID, targetUsername, remark)
	if err != nil {
		t.Fatalf("Apply(%q->%q): %v", applicantID, targetUsername, err)
	}
	return tid
}

func liveRow(t *testing.T, db *gorm.DB, uid, fid string) *store.Friendship {
	t.Helper()
	var r store.Friendship
	if err := db.Where("user_id = ? AND friend_id = ?", uid, fid).First(&r).Error; err != nil {
		t.Fatalf("liveRow %s->%s: %v", uid, fid, err)
	}
	return &r
}

func unscopedCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Unscoped().Model(&store.Friendship{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func liveNow() time.Time { return time.Now() }

// ---------- tests ----------

func TestApplyPendingAndNotify(t *testing.T) {
	svc, db, em, iv := newFriendTestService(t)
	createFriendUser(t, db, "u_alice", "alice")
	createFriendUser(t, db, "u_bob", "bob")

	tid, err := svc.Apply("u_alice", "bob", "hi bob")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if tid != "u_bob" {
		t.Fatalf("target id = %q, want u_bob", tid)
	}
	// Both directions pending.
	a := liveRow(t, db, "u_alice", "u_bob")
	b := liveRow(t, db, "u_bob", "u_alice")
	if a.Status != "pending" || b.Status != "pending" {
		t.Fatalf("statuses = %q/%q, want pending/pending", a.Status, b.Status)
	}
	if a.InitiatorID != "u_alice" || b.InitiatorID != "u_alice" {
		t.Fatalf("initiators = %q/%q, want u_alice", a.InitiatorID, b.InitiatorID)
	}
	// Notify emitted to target.
	got := em.byType(pb.MsgType_FRIEND_APPLY_NOTIFY)
	if len(got) != 1 {
		t.Fatalf("apply notifies = %d, want 1", len(got))
	}
	if got[0].UserID != "u_bob" || got[0].Msg.GetToUid() != "u_bob" || got[0].Msg.GetFromUid() != "u_alice" {
		t.Fatalf("apply notify routing wrong: %+v msg=%v", got[0].UserID, got[0].Msg)
	}
	// Invalidated.
	if !iv.has(session.BuildPrivateCovID("u_alice", "u_bob")) {
		t.Fatalf("apply did not invalidate %q (got %v)", session.BuildPrivateCovID("u_alice", "u_bob"), iv.keys)
	}
}

func TestAcceptBothRowsNotifyInvalidate(t *testing.T) {
	svc, db, em, iv := newFriendTestService(t)
	createFriendUser(t, db, "u_alice", "alice")
	createFriendUser(t, db, "u_bob", "bob")
	mustApply(t, svc, "u_alice", "bob", "hello")

	emBefore := em.count()
	_ = emBefore
	ivBefore := iv.count()

	if err := svc.Respond("u_bob", "u_alice", "accept"); err != nil {
		t.Fatalf("Respond accept: %v", err)
	}
	a := liveRow(t, db, "u_alice", "u_bob")
	b := liveRow(t, db, "u_bob", "u_alice")
	if a.Status != "accepted" || b.Status != "accepted" {
		t.Fatalf("statuses = %q/%q, want accepted/accepted", a.Status, b.Status)
	}
	got := em.byType(pb.MsgType_FRIEND_ACCEPT_NOTIFY)
	if len(got) != 1 {
		t.Fatalf("accept notifies = %d, want 1", len(got))
	}
	if got[0].UserID != "u_alice" || got[0].Msg.GetToUid() != "u_alice" || got[0].Msg.GetFromUid() != "u_bob" {
		t.Fatalf("accept notify routing wrong: user=%q msg=%v", got[0].UserID, got[0].Msg)
	}
	if iv.count() <= ivBefore {
		t.Fatal("accept did not invalidate")
	}
	if !iv.has(session.BuildPrivateCovID("u_alice", "u_bob")) {
		t.Fatalf("accept missing invalidate key, got %v", iv.keys)
	}
	// Friends visible both sides.
	fa, err := svc.ListFriends("u_alice")
	if err != nil || len(fa) != 1 || fa[0].UserID != "u_bob" || fa[0].Username != "bob" {
		t.Fatalf("ListFriends alice: %+v err=%v", fa, err)
	}
	fb, err := svc.ListFriends("u_bob")
	if err != nil || len(fb) != 1 || fb[0].UserID != "u_alice" {
		t.Fatalf("ListFriends bob: %+v err=%v", fb, err)
	}
}

func TestReject(t *testing.T) {
	svc, db, em, _ := newFriendTestService(t)
	createFriendUser(t, db, "u_alice", "alice")
	createFriendUser(t, db, "u_bob", "bob")
	mustApply(t, svc, "u_alice", "bob", "")

	if err := svc.Respond("u_bob", "u_alice", "reject"); err != nil {
		t.Fatalf("Respond reject: %v", err)
	}
	a := liveRow(t, db, "u_alice", "u_bob")
	b := liveRow(t, db, "u_bob", "u_alice")
	if a.Status != "rejected" || b.Status != "rejected" {
		t.Fatalf("statuses = %q/%q, want rejected/rejected", a.Status, b.Status)
	}
	// Reject emits no accept notify.
	if len(em.byType(pb.MsgType_FRIEND_ACCEPT_NOTIFY)) != 0 {
		t.Fatal("reject must not emit accept notify")
	}
	// No friends.
	fa, _ := svc.ListFriends("u_alice")
	if len(fa) != 0 {
		t.Fatalf("rejected pair must not list as friends: %+v", fa)
	}
}

func TestDeleteBothSoftDeletedAndInvalidated(t *testing.T) {
	svc, db, _, iv := newFriendTestService(t)
	createFriendUser(t, db, "u_alice", "alice")
	createFriendUser(t, db, "u_bob", "bob")
	mustApply(t, svc, "u_alice", "bob", "")
	if err := svc.Respond("u_bob", "u_alice", "accept"); err != nil {
		t.Fatal(err)
	}
	before := iv.count()
	if err := svc.Delete("u_alice", "u_bob"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Both rows soft-deleted: live query finds none, unscoped finds deleted.
	var n int64
	if err := db.Model(&store.Friendship{}).Where(
		"(user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?)",
		"u_alice", "u_bob", "u_bob", "u_alice",
	).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("live rows after delete = %d, want 0", n)
	}
	var all []store.Friendship
	if err := db.Unscoped().Where(
		"(user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?)",
		"u_alice", "u_bob", "u_bob", "u_alice",
	).Find(&all).Error; err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("unscoped rows = %d, want 2", len(all))
	}
	for _, r := range all {
		if !r.DeletedAt.Valid {
			t.Fatalf("row %s->%s not soft-deleted", r.UserID, r.FriendID)
		}
	}
	if iv.count() <= before {
		t.Fatal("delete did not invalidate")
	}
	if !iv.has(session.BuildPrivateCovID("u_alice", "u_bob")) {
		t.Fatalf("delete missing invalidate, got %v", iv.keys)
	}
	// Deleting again → 30001.
	if err := svc.Delete("u_alice", "u_bob"); CodeOf(err) != CodeFriendNotFound {
		t.Fatalf("delete twice: got %v, want 30001", err)
	}
}

func TestReapplyAfterDeleteRevives(t *testing.T) {
	svc, db, _, _ := newFriendTestService(t)
	createFriendUser(t, db, "u_alice", "alice")
	createFriendUser(t, db, "u_bob", "bob")
	mustApply(t, svc, "u_alice", "bob", "first")
	if err := svc.Respond("u_bob", "u_alice", "accept"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete("u_alice", "u_bob"); err != nil {
		t.Fatal(err)
	}
	if n := unscopedCount(t, db); n != 2 {
		t.Fatalf("before re-apply unscoped = %d, want 2", n)
	}
	// Re-apply must revive without unique violation.
	if _, err := svc.Apply("u_alice", "bob", "again"); err != nil {
		t.Fatalf("re-apply after delete: %v", err)
	}
	if n := unscopedCount(t, db); n != 2 {
		t.Fatalf("after re-apply unscoped = %d, want still 2 (revive, not insert)", n)
	}
	a := liveRow(t, db, "u_alice", "u_bob")
	b := liveRow(t, db, "u_bob", "u_alice")
	if a.Status != "pending" || b.Status != "pending" {
		t.Fatalf("revived statuses = %q/%q, want pending", a.Status, b.Status)
	}
	if a.DeletedAt.Valid || b.DeletedAt.Valid {
		t.Fatal("revived rows must clear deleted_at")
	}
}

func TestReapplyAfterRejectRevives(t *testing.T) {
	svc, db, _, _ := newFriendTestService(t)
	createFriendUser(t, db, "u_alice", "alice")
	createFriendUser(t, db, "u_bob", "bob")
	mustApply(t, svc, "u_alice", "bob", "")
	if err := svc.Respond("u_bob", "u_alice", "reject"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply("u_alice", "bob", "retry"); err != nil {
		t.Fatalf("re-apply after reject: %v", err)
	}
	if n := unscopedCount(t, db); n != 2 {
		t.Fatalf("unscoped = %d, want 2", n)
	}
	a := liveRow(t, db, "u_alice", "u_bob")
	if a.Status != "pending" {
		t.Fatalf("status = %q, want pending", a.Status)
	}
}

func TestDuplicatePendingApply(t *testing.T) {
	svc, db, _, _ := newFriendTestService(t)
	createFriendUser(t, db, "u_alice", "alice")
	createFriendUser(t, db, "u_bob", "bob")
	mustApply(t, svc, "u_alice", "bob", "")
	// Same direction duplicate.
	if _, err := svc.Apply("u_alice", "bob", "again"); CodeOf(err) != CodeFriendAlready {
		t.Fatalf("dup apply: got %v, want 30003", err)
	}
	// Reverse direction while pending also blocked.
	if _, err := svc.Apply("u_bob", "alice", "reverse"); CodeOf(err) != CodeFriendAlready {
		t.Fatalf("reverse pending apply: got %v, want 30003", err)
	}
	// Already accepted also 30003.
	if err := svc.Respond("u_bob", "u_alice", "accept"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply("u_alice", "bob", "x"); CodeOf(err) != CodeFriendAlready {
		t.Fatalf("apply when accepted: got %v, want 30003", err)
	}
	_ = db
}

func TestSelfApply(t *testing.T) {
	svc, db, _, _ := newFriendTestService(t)
	createFriendUser(t, db, "u_alice", "alice")
	if _, err := svc.Apply("u_alice", "alice", ""); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("self-apply: got %v, want 10001", err)
	}
	_ = db
}

func TestApplyUnknown(t *testing.T) {
	svc, db, _, _ := newFriendTestService(t)
	createFriendUser(t, db, "u_alice", "alice")
	if _, err := svc.Apply("u_alice", "ghost", ""); CodeOf(err) != CodeUserNotFound {
		t.Fatalf("unknown target: got %v, want 20001", err)
	}
	_ = db
}

func TestRespondWrongPartyAndNonPending(t *testing.T) {
	svc, db, _, _ := newFriendTestService(t)
	createFriendUser(t, db, "u_alice", "alice")
	createFriendUser(t, db, "u_bob", "bob")
	createFriendUser(t, db, "u_carol", "carol")
	mustApply(t, svc, "u_alice", "bob", "")

	// Wrong party: initiator tries to respond to own request.
	if err := svc.Respond("u_alice", "u_bob", "accept"); CodeOf(err) != CodeFriendNotFound {
		t.Fatalf("initiator self-respond: got %v, want 30001", err)
	}
	// Third party with no relation.
	if err := svc.Respond("u_carol", "u_alice", "accept"); CodeOf(err) != CodeFriendNotFound {
		t.Fatalf("third-party respond: got %v, want 30001", err)
	}
	// Non-existent pair entirely.
	if err := svc.Respond("u_bob", "u_carol", "accept"); CodeOf(err) != CodeFriendNotFound {
		t.Fatalf("non-pending respond: got %v, want 30001", err)
	}
	// Self respond.
	if err := svc.Respond("u_bob", "u_bob", "accept"); CodeOf(err) != CodeFriendNotFound {
		t.Fatalf("self respond: got %v, want 30001", err)
	}
	_ = db
}

func TestRespondInvalidAction(t *testing.T) {
	svc, db, _, _ := newFriendTestService(t)
	createFriendUser(t, db, "u_alice", "alice")
	createFriendUser(t, db, "u_bob", "bob")
	mustApply(t, svc, "u_alice", "bob", "")
	if err := svc.Respond("u_bob", "u_alice", "maybe"); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("bad action: got %v, want 10001", err)
	}
	_ = db
}

func TestBlockedApply(t *testing.T) {
	svc, db, _, _ := newFriendTestService(t)
	createFriendUser(t, db, "u_alice", "alice")
	createFriendUser(t, db, "u_bob", "bob")
	now := liveNow()
	// Seed a live blocked relation (bob blocked alice).
	if err := db.Create(&store.Friendship{UserID: "u_alice", FriendID: "u_bob", InitiatorID: "u_bob", Status: "blocked", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&store.Friendship{UserID: "u_bob", FriendID: "u_alice", InitiatorID: "u_bob", Status: "blocked", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply("u_alice", "bob", ""); CodeOf(err) != CodeFriendBlocked {
		t.Fatalf("blocked apply: got %v, want 30002", err)
	}
}

func TestListFriendsAndPending(t *testing.T) {
	svc, db, _, _ := newFriendTestService(t)
	createFriendUser(t, db, "u_alice", "alice")
	createFriendUser(t, db, "u_bob", "bob")
	createFriendUser(t, db, "u_carol", "carol")
	// Give bob profile fields.
	if err := db.Model(&store.User{}).Where("id = ?", "u_bob").Updates(map[string]interface{}{
		"avatar_url": "http://x/b.png", "signature": "hey",
	}).Error; err != nil {
		t.Fatal(err)
	}

	fa, err := svc.ListFriends("u_alice")
	if err != nil || len(fa) != 0 {
		t.Fatalf("empty friends: %+v err=%v", fa, err)
	}
	rq, err := svc.ListPending("u_bob")
	if err != nil || len(rq) != 0 {
		t.Fatalf("empty pending: %+v err=%v", rq, err)
	}

	mustApply(t, svc, "u_alice", "bob", "pls")
	mustApply(t, svc, "u_carol", "bob", "hi")

	// Target bob sees 2 pending incoming requests
	pend, err := svc.ListPending("u_bob")
	if err != nil || len(pend) != 2 {
		t.Fatalf("pending bob: %+v err=%v", pend, err)
	}

	// Applicants (alice, carol) must NOT see outgoing requests in their incoming pending list
	pendAlice, err := svc.ListPending("u_alice")
	if err != nil || len(pendAlice) != 0 {
		t.Fatalf("applicant alice should have 0 incoming pending requests, got: %+v", pendAlice)
	}
	pendCarol, err := svc.ListPending("u_carol")
	if err != nil || len(pendCarol) != 0 {
		t.Fatalf("applicant carol should have 0 incoming pending requests, got: %+v", pendCarol)
	}
	byUser := map[string]PendingItem{}
	for _, p := range pend {
		byUser[p.UserID] = p
	}
	if byUser["u_alice"].Username != "alice" || byUser["u_alice"].Remark != "pls" {
		t.Fatalf("alice pending row wrong: %+v", byUser["u_alice"])
	}
	if byUser["u_carol"].Username != "carol" {
		t.Fatalf("carol pending row wrong: %+v", byUser["u_carol"])
	}

	if err := svc.Respond("u_bob", "u_alice", "accept"); err != nil {
		t.Fatal(err)
	}
	fa, _ = svc.ListFriends("u_alice")
	if len(fa) != 1 || fa[0].Username != "bob" || fa[0].AvatarURL != "http://x/b.png" || fa[0].Signature != "hey" {
		t.Fatalf("friends alice after accept: %+v", fa)
	}
	// Pending for bob now only carol.
	pend, _ = svc.ListPending("u_bob")
	if len(pend) != 1 || pend[0].UserID != "u_carol" {
		t.Fatalf("pending after accept: %+v", pend)
	}
}

func TestDeleteNonExistent(t *testing.T) {
	svc, db, _, _ := newFriendTestService(t)
	createFriendUser(t, db, "u_alice", "alice")
	createFriendUser(t, db, "u_bob", "bob")
	if err := svc.Delete("u_alice", "u_bob"); CodeOf(err) != CodeFriendNotFound {
		t.Fatalf("delete non-friend: got %v, want 30001", err)
	}
}
