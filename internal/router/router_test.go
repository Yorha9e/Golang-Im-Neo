package router

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	pb "golang-im-neo-system/proto"

	"golang-im-neo-system/internal/logic/session"
	"golang-im-neo-system/internal/store"
)

// ---------- fakes ----------

var (
	_ Emitter   = (*fakeEmitter)(nil)
	_ Persister = (*fakePersister)(nil)
)

// fakeEmitter records all outbound calls, grouped by recipient.
type fakeEmitter struct {
	mu         sync.Mutex
	toUser     map[string][][]byte
	broadcasts [][]byte
	kicked     []string
	offline    map[string]bool
}

func newFakeEmitter() *fakeEmitter {
	return &fakeEmitter{toUser: make(map[string][][]byte)}
}

func (f *fakeEmitter) SendToUser(userID string, msg []byte) bool {
	cp := append([]byte(nil), msg...)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.toUser[userID] = append(f.toUser[userID], cp)
	return true
}

func (f *fakeEmitter) Broadcast(msg []byte) {
	cp := append([]byte(nil), msg...)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broadcasts = append(f.broadcasts, cp)
}

func (f *fakeEmitter) IsOnline(userID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.offline[userID]
}

// setOffline toggles simulated presence (default: everybody online).
// SendToUsers skips offline users; SendToUser still records (unit-test
// visibility into pushes regardless of presence).
func (f *fakeEmitter) setOffline(userID string, off bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.offline == nil {
		f.offline = make(map[string]bool)
	}
	if off {
		f.offline[userID] = true
	} else {
		delete(f.offline, userID)
	}
}

// SendToUsers records one copy per online listed user (M2 group fan-out fake).
func (f *fakeEmitter) SendToUsers(userIDs []string, msg []byte) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, id := range userIDs {
		if f.offline[id] {
			continue
		}
		f.toUser[id] = append(f.toUser[id], append([]byte(nil), msg...))
		n++
	}
	return n
}

func (f *fakeEmitter) KickUser(userID, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kicked = append(f.kicked, userID)
}

func (f *fakeEmitter) framesFor(userID string) [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.toUser[userID]...)
}

func (f *fakeEmitter) broadcastsCopy() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.broadcasts...)
}

// fakePersister records persisted rows; default echoes msg seq/ts as success.
// fn, when set, overrides the result (error / Duplicated replay paths).
type fakePersister struct {
	mu    sync.Mutex
	calls []*store.Message
	fn    func(m *store.Message) WriteResult
}

func (f *fakePersister) EnqueueSync(m *store.Message) <-chan WriteResult {
	f.mu.Lock()
	f.calls = append(f.calls, m)
	fn := f.fn
	f.mu.Unlock()
	ch := make(chan WriteResult, 1)
	if fn != nil {
		ch <- fn(m)
	} else {
		ch <- WriteResult{Seq: m.Seq, Timestamp: m.Timestamp}
	}
	return ch
}

func (f *fakePersister) numCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakePersister) lastCall() *store.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

// ---------- harness ----------

func newTestSetup(t *testing.T) (*Router, *gorm.DB, *fakeEmitter, *fakePersister) {
	t.Helper()
	db, err := store.NewDB(store.DBConfig{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	alloc := session.NewAllocator(db)
	em := newFakeEmitter()
	ps := &fakePersister{}
	r := New(em, ps, alloc, db, zap.NewNop())
	return r, db, em, ps
}

// seedFriends inserts the physical bidirectional accepted rows for a<->b.
func seedFriends(t *testing.T, db *gorm.DB, a, b string) {
	t.Helper()
	for _, p := range [][2]string{{a, b}, {b, a}} {
		if err := db.Create(&store.Friendship{
			UserID: p[0], FriendID: p[1], InitiatorID: p[0], Status: "accepted",
		}).Error; err != nil {
			t.Fatalf("seed friendship %v: %v", p, err)
		}
	}
}

func mustFrame(t *testing.T, m *pb.WsMessage) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func mustDecode(t *testing.T, b []byte) *pb.WsMessage {
	t.Helper()
	m := &pb.WsMessage{}
	if err := proto.Unmarshal(b, m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// decodedFrames returns all frames sent to userID, decoded.
func decodedFrames(t *testing.T, em *fakeEmitter, userID string) []*pb.WsMessage {
	t.Helper()
	raw := em.framesFor(userID)
	out := make([]*pb.WsMessage, 0, len(raw))
	for _, b := range raw {
		out = append(out, mustDecode(t, b))
	}
	return out
}

func filterType(frames []*pb.WsMessage, typ pb.MsgType) []*pb.WsMessage {
	var out []*pb.WsMessage
	for _, f := range frames {
		if f.GetType() == typ {
			out = append(out, f)
		}
	}
	return out
}

func noticeCode(t *testing.T, f *pb.WsMessage) int {
	t.Helper()
	if f.GetType() != pb.MsgType_SYSTEM_NOTICE {
		t.Fatalf("expected SYSTEM_NOTICE, got %v", f.GetType())
	}
	var e noticeExtra
	if err := json.Unmarshal([]byte(f.GetExtra()), &e); err != nil {
		t.Fatalf("notice Extra is not coded JSON %q: %v", f.GetExtra(), err)
	}
	return e.Code
}

func saneMillis(t *testing.T, ts int64) {
	t.Helper()
	now := time.Now().UnixMilli()
	if d := now - ts; d < 0 || d > 60_000 {
		t.Fatalf("timestamp %d not sane vs now %d", ts, now)
	}
}

// ---------- mission test 2: private pipeline happy path ----------

func TestPrivatePipelineHappyPath(t *testing.T) {
	r, _, em, ps := newTestSetup(t)
	seedFriends(t, r.db, "u_a", "u_b")

	r.HandleInbound("u_a", "sess-1", "interactive", mustFrame(t, &pb.WsMessage{
		Type: pb.MsgType_PRIVATE_CHAT, FromUid: "u_a", ToUid: "u_b",
		Content: "hello", StanzaId: "s-1",
	}))

	if got := ps.numCalls(); got != 1 {
		t.Fatalf("persister calls = %d, want 1", got)
	}
	row := ps.lastCall()
	if row.CovID != "cov:u_a:u_b" {
		t.Fatalf("CovID = %q, want colon dict order cov:u_a:u_b", row.CovID)
	}
	if row.Seq != 1 || row.ChatType != "chat" || row.FromUID != "u_a" || row.ToUID != "u_b" {
		t.Fatalf("row identity wrong: %+v", row)
	}
	if row.Content != "hello" || row.StanzaID != "s-1" || row.ContentType != 1 || row.Status != 1 {
		t.Fatalf("row payload wrong: %+v", row)
	}
	if row.CreatedAt.IsZero() || time.Since(row.CreatedAt) > time.Minute {
		t.Fatalf("row CreatedAt insane: %v", row.CreatedAt)
	}
	saneMillis(t, row.Timestamp)

	acks := filterType(decodedFrames(t, em, "u_a"), pb.MsgType_ACK)
	if len(acks) != 1 {
		t.Fatalf("sender ACKs = %d, want 1", len(acks))
	}
	ack := acks[0]
	if ack.GetSeq() != 1 || ack.GetStanzaId() != "s-1" || ack.GetToUid() != "u_a" {
		t.Fatalf("ACK wrong: %v", ack)
	}
	if ack.GetTimestamp() != row.Timestamp {
		t.Fatalf("ACK ts %d != persisted ts %d", ack.GetTimestamp(), row.Timestamp)
	}

	pushes := decodedFrames(t, em, "u_b")
	if len(pushes) != 1 {
		t.Fatalf("receiver pushes = %d, want 1", len(pushes))
	}
	push := pushes[0]
	if push.GetType() != pb.MsgType_PRIVATE_CHAT || push.GetSeq() != 1 ||
		push.GetContent() != "hello" || push.GetFromUid() != "u_a" ||
		push.GetToUid() != "u_b" || push.GetStanzaId() != "s-1" {
		t.Fatalf("push wrong: %v", push)
	}
}

// ---------- mission test 1: colon dict order ----------

func TestPrivateCovIDColonDictOrder(t *testing.T) {
	r, _, _, ps := newTestSetup(t)
	seedFriends(t, r.db, "u_a", "u_b")

	// Sender is the lexicographically LARGER uid; covID must still sort ascending.
	r.HandleInbound("u_b", "sess-9", "interactive", mustFrame(t, &pb.WsMessage{
		Type: pb.MsgType_PRIVATE_CHAT, ToUid: "u_a", Content: "hi", StanzaId: "s-x",
	}))
	if got := ps.numCalls(); got != 1 {
		t.Fatalf("persister calls = %d, want 1", got)
	}
	if cov := ps.lastCall().CovID; cov != "cov:u_a:u_b" {
		t.Fatalf("CovID = %q, want cov:u_a:u_b", cov)
	}
}

// ---------- mission test 3: dedup window replay ----------

func TestDedupReplaySameStanza(t *testing.T) {
	r, _, em, ps := newTestSetup(t)
	seedFriends(t, r.db, "u_a", "u_b")

	frame := mustFrame(t, &pb.WsMessage{
		Type: pb.MsgType_PRIVATE_CHAT, ToUid: "u_b", Content: "hello", StanzaId: "s-dup",
	})
	r.HandleInbound("u_a", "sess-1", "interactive", frame)

	// Client retry with same stanza (different content must NOT matter).
	retry := mustFrame(t, &pb.WsMessage{
		Type: pb.MsgType_PRIVATE_CHAT, ToUid: "u_b", Content: "hello-again", StanzaId: "s-dup",
	})
	r.HandleInbound("u_a", "sess-1", "interactive", retry)

	if got := ps.numCalls(); got != 1 {
		t.Fatalf("dedup replay persisted %d times, want exactly 1", got)
	}
	if got := len(decodedFrames(t, em, "u_b")); got != 1 {
		t.Fatalf("dedup replay pushed %d times, want exactly 1", got)
	}
	acks := filterType(decodedFrames(t, em, "u_a"), pb.MsgType_ACK)
	if len(acks) != 2 {
		t.Fatalf("sender ACKs = %d, want 2 (original + replay)", len(acks))
	}
	if acks[0].GetSeq() != acks[1].GetSeq() || acks[0].GetSeq() != 1 {
		t.Fatalf("replay ACK must carry SAME seq 1, got %d and %d",
			acks[0].GetSeq(), acks[1].GetSeq())
	}
	if acks[1].GetStanzaId() != "s-dup" {
		t.Fatalf("replay ACK stanza = %q, want s-dup", acks[1].GetStanzaId())
	}
}

// ---------- mission test 4: post-restart Duplicated recovery ----------

func TestPostRestartDuplicatedRecovery(t *testing.T) {
	r, _, em, ps := newTestSetup(t)
	seedFriends(t, r.db, "u_a", "u_b")

	// Simulate persister post-restart replay: row exists with seq 7.
	ps.fn = func(m *store.Message) WriteResult {
		return WriteResult{Seq: 7, Timestamp: 1234567890123, Duplicated: true}
	}
	r.HandleInbound("u_a", "sess-1", "interactive", mustFrame(t, &pb.WsMessage{
		Type: pb.MsgType_PRIVATE_CHAT, ToUid: "u_b", Content: "retry-after-crash", StanzaId: "s-crash",
	}))

	acks := filterType(decodedFrames(t, em, "u_a"), pb.MsgType_ACK)
	if len(acks) != 1 {
		t.Fatalf("ACKs = %d, want 1", len(acks))
	}
	if acks[0].GetSeq() != 7 || acks[0].GetTimestamp() != 1234567890123 {
		t.Fatalf("recovery ACK must carry (7, ts), got (%d, %d)",
			acks[0].GetSeq(), acks[0].GetTimestamp())
	}
	if got := len(decodedFrames(t, em, "u_b")); got != 0 {
		t.Fatalf("Duplicated path must NOT push, got %d pushes", got)
	}

	// The recovered mapping is now in the window: a further retry with a
	// healthy persister must replay seq 7 without persisting again.
	ps.fn = nil
	r.HandleInbound("u_a", "sess-1", "interactive", mustFrame(t, &pb.WsMessage{
		Type: pb.MsgType_PRIVATE_CHAT, ToUid: "u_b", Content: "retry-again", StanzaId: "s-crash",
	}))
	if got := ps.numCalls(); got != 1 {
		t.Fatalf("window replay after recovery persisted, calls = %d, want 1", got)
	}
	acks = filterType(decodedFrames(t, em, "u_a"), pb.MsgType_ACK)
	if len(acks) != 2 || acks[1].GetSeq() != 7 {
		t.Fatalf("second ACK must replay seq 7, got %v", acks)
	}
}

// ---------- mission test 5: friendship gate + cache ----------

func TestFriendshipGate(t *testing.T) {
	t.Run("allowed with accepted rows", func(t *testing.T) {
		r, _, em, ps := newTestSetup(t)
		seedFriends(t, r.db, "u_a", "u_b")
		r.HandleInbound("u_a", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_PRIVATE_CHAT, ToUid: "u_b", Content: "hi", StanzaId: "s-1",
		}))
		if got := ps.numCalls(); got != 1 {
			t.Fatalf("allowed chat must persist, calls = %d", got)
		}
		if got := len(decodedFrames(t, em, "u_b")); got != 1 {
			t.Fatalf("allowed chat must push, pushes = %d", got)
		}
	})

	t.Run("denied without rows: 30001, no persist, no push", func(t *testing.T) {
		r, _, em, ps := newTestSetup(t)
		r.HandleInbound("u_x", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_PRIVATE_CHAT, ToUid: "u_y", Content: "hi", StanzaId: "s-1",
		}))
		if got := ps.numCalls(); got != 0 {
			t.Fatalf("denied chat must not persist, calls = %d", got)
		}
		if got := len(decodedFrames(t, em, "u_y")); got != 0 {
			t.Fatalf("denied chat must not push, pushes = %d", got)
		}
		frames := decodedFrames(t, em, "u_x")
		if len(frames) != 1 {
			t.Fatalf("sender must get exactly 1 notice, got %d", len(frames))
		}
		if code := noticeCode(t, frames[0]); code != CodeFriendNotFound {
			t.Fatalf("notice code = %d, want 30001", code)
		}
		if frames[0].GetContent() == "" {
			t.Fatal("notice must carry a human hint in Content")
		}
	})

	t.Run("second call served from cache after rows deleted", func(t *testing.T) {
		r, db, _, ps := newTestSetup(t)
		seedFriends(t, db, "u_a", "u_b")
		first := mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_PRIVATE_CHAT, ToUid: "u_b", Content: "hi", StanzaId: "s-1",
		})
		r.HandleInbound("u_a", "s", "interactive", first)
		if got := ps.numCalls(); got != 1 {
			t.Fatalf("first call must persist, calls = %d", got)
		}
		// Remove the DB rows: a cache hit must still allow the second call.
		if err := db.Where("user_id IN ?", []string{"u_a", "u_b"}).Delete(&store.Friendship{}).Error; err != nil {
			t.Fatalf("delete friendships: %v", err)
		}
		r.HandleInbound("u_a", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_PRIVATE_CHAT, ToUid: "u_b", Content: "hi2", StanzaId: "s-2",
		}))
		if got := ps.numCalls(); got != 2 {
			t.Fatalf("cached verdict must allow without DB rows, calls = %d, want 2", got)
		}
	})

	t.Run("broadcast skips friendship check", func(t *testing.T) {
		r, _, _, ps := newTestSetup(t)
		// No friendship rows at all: CHAT must still flow.
		r.HandleInbound("u_a", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_CHAT, Content: "hall hi", StanzaId: "s-h",
		}))
		if got := ps.numCalls(); got != 1 {
			t.Fatalf("broadcast must persist without friendships, calls = %d", got)
		}
	})
}

// ---------- mission test 6: rate limit ----------

func TestRateLimitDropsWithWarningDistinctStanzas(t *testing.T) {
	r, _, em, ps := newTestSetup(t)

	const total = 25
	for i := 0; i < total; i++ {
		r.HandleInbound("u_flooder", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_CHAT, Content: "spam",
			StanzaId: "spam-" + string(rune('a'+i/10)) + string(rune('0'+i%10)),
		}))
	}

	allowed := ps.numCalls()
	senderFrames := decodedFrames(t, em, "u_flooder")
	notices := filterType(senderFrames, pb.MsgType_SYSTEM_NOTICE)
	if allowed+len(notices) != total {
		t.Fatalf("allowed(%d) + warnings(%d) != %d sent", allowed, len(notices), total)
	}
	if len(notices) == 0 {
		t.Fatal("rate-limited messages must each emit a warning frame")
	}
	if allowed >= total {
		t.Fatalf("expected some drops under 25 rapid messages, allowed = %d", allowed)
	}
	for _, n := range notices {
		if n.GetContent() == "" {
			t.Fatal("warning frame must carry a human hint")
		}
	}
}

// ---------- mission test 7: heartbeat ----------

func TestHeartbeatPong(t *testing.T) {
	r, _, em, ps := newTestSetup(t)

	r.HandleInbound("u_a", "sess-1", "interactive", mustFrame(t, &pb.WsMessage{
		Type: pb.MsgType_HEARTBEAT_PING,
	}))

	if got := ps.numCalls(); got != 0 {
		t.Fatalf("heartbeat must not persist, calls = %d", got)
	}
	if got := len(em.broadcastsCopy()); got != 0 {
		t.Fatalf("heartbeat must not broadcast, got %d", got)
	}
	frames := decodedFrames(t, em, "u_a")
	if len(frames) != 1 || frames[0].GetType() != pb.MsgType_HEARTBEAT_PONG {
		t.Fatalf("expected exactly 1 PONG, got %v", frames)
	}
}

// ---------- mission test 8: identity overwrite ----------

func TestIdentityOverwrite(t *testing.T) {
	r, _, em, ps := newTestSetup(t)
	seedFriends(t, r.db, "u_real", "u_b")

	// Spoofed from_uid + client-set seq/ts must all be replaced server-side.
	r.HandleInbound("u_real", "sess-1", "interactive", mustFrame(t, &pb.WsMessage{
		Type: pb.MsgType_PRIVATE_CHAT, FromUid: "u_attacker", ToUid: "u_b",
		Content: "spoof", StanzaId: "s-spoof", Seq: 999, Timestamp: 111,
	}))

	if got := ps.numCalls(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	row := ps.lastCall()
	if row.FromUID != "u_real" {
		t.Fatalf("persisted FromUID = %q, want u_real", row.FromUID)
	}
	if row.Seq != 1 {
		t.Fatalf("persisted Seq = %d, want server-allocated 1", row.Seq)
	}
	if row.Timestamp == 111 {
		t.Fatal("persisted Timestamp must be server-assigned, not client 111")
	}
	saneMillis(t, row.Timestamp)

	pushes := decodedFrames(t, em, "u_b")
	if len(pushes) != 1 || pushes[0].GetFromUid() != "u_real" {
		t.Fatalf("push must carry real sender, got %v", pushes)
	}
}

// ---------- broadcast pipeline ----------

func TestBroadcastPipeline(t *testing.T) {
	r, _, em, ps := newTestSetup(t)

	r.HandleInbound("u_a", "sess-1", "interactive", mustFrame(t, &pb.WsMessage{
		Type: pb.MsgType_CHAT, Content: "hi all", StanzaId: "s-hall",
	}))

	if got := ps.numCalls(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	row := ps.lastCall()
	if row.CovID != PublicHallCovID || row.ChatType != "groupchat" {
		t.Fatalf("hall row wrong: cov=%q chatType=%q", row.CovID, row.ChatType)
	}

	casts := em.broadcastsCopy()
	if len(casts) != 1 {
		t.Fatalf("broadcasts = %d, want 1", len(casts))
	}
	out := mustDecode(t, casts[0])
	if out.GetType() != pb.MsgType_CHAT || out.GetContent() != "hi all" ||
		out.GetFromUid() != "u_a" || out.GetSeq() != 1 || out.GetStanzaId() != "s-hall" {
		t.Fatalf("broadcast frame wrong: %v", out)
	}

	acks := filterType(decodedFrames(t, em, "u_a"), pb.MsgType_ACK)
	if len(acks) != 1 || acks[0].GetSeq() != 1 || acks[0].GetStanzaId() != "s-hall" {
		t.Fatalf("sender ACK wrong: %v", acks)
	}
}

// ---------- persist failure => 50001, no push ----------

func TestPersistErrorYields50001(t *testing.T) {
	r, _, em, ps := newTestSetup(t)
	seedFriends(t, r.db, "u_a", "u_b")
	ps.fn = func(m *store.Message) WriteResult {
		return WriteResult{Err: errors.New("disk boom")}
	}

	r.HandleInbound("u_a", "s", "interactive", mustFrame(t, &pb.WsMessage{
		Type: pb.MsgType_PRIVATE_CHAT, ToUid: "u_b", Content: "hi", StanzaId: "s-e",
	}))

	frames := decodedFrames(t, em, "u_a")
	if len(frames) != 1 {
		t.Fatalf("sender must get exactly 1 notice, got %d", len(frames))
	}
	if code := noticeCode(t, frames[0]); code != CodeInternalError {
		t.Fatalf("notice code = %d, want 50001", code)
	}
	if got := len(decodedFrames(t, em, "u_b")); got != 0 {
		t.Fatalf("failed persist must not push, got %d", got)
	}
}

// ---------- unsupported types + malformed frames are dropped safely ----------

func TestUnsupportedTypesDropped(t *testing.T) {
	r, _, em, ps := newTestSetup(t)

	// NOTE (M2): GROUP_CHAT and SIGNALING_* are routed now (see
	// group_signaling_test.go); only truly unhandled types drop here.
	for _, typ := range []pb.MsgType{
		pb.MsgType_ACK,
		pb.MsgType_UNKNOWN,
	} {
		r.HandleInbound("u_a", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: typ, ToUid: "u_b", Content: "x", StanzaId: "s-" + typ.String(),
		}))
	}
	if got := ps.numCalls(); got != 0 {
		t.Fatalf("unsupported types must not persist, calls = %d", got)
	}
	if got := len(decodedFrames(t, em, "u_a")) + len(decodedFrames(t, em, "u_b")); got != 0 {
		t.Fatalf("unsupported types must emit nothing, got %d frames", got)
	}
}

func TestMalformedFrameDropped(t *testing.T) {
	r, _, _, ps := newTestSetup(t)
	r.HandleInbound("u_a", "s", "interactive", []byte{0xff, 0x00, 0x01, 0x02})
	if got := ps.numCalls(); got != 0 {
		t.Fatalf("malformed frame must not persist, calls = %d", got)
	}
}

// ---------- full pipeline over a REAL sqlite DB + allocator ----------

func TestPipelineAgainstRealStore(t *testing.T) {
	r, db, em, ps := newTestSetup(t)
	seedFriends(t, db, "u_a", "u_b")

	for i := 1; i <= 3; i++ {
		r.HandleInbound("u_a", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_PRIVATE_CHAT, ToUid: "u_b", Content: "m",
			StanzaId: "real-" + string(rune('0'+i)),
		}))
	}
	if got := ps.numCalls(); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}
	// Seqs are per-cov monotonic via the real allocator (fresh DB => 1,2,3).
	for i, want := range []int64{1, 2, 3} {
		if ps.calls[i].Seq != want {
			t.Fatalf("call %d seq = %d, want %d", i, ps.calls[i].Seq, want)
		}
	}
	acks := filterType(decodedFrames(t, em, "u_a"), pb.MsgType_ACK)
	if len(acks) != 3 {
		t.Fatalf("ACKs = %d, want 3", len(acks))
	}
	var conv store.Conversation
	if err := db.Where("cov_id = ?", "cov:u_a:u_b").First(&conv).Error; err != nil {
		t.Fatalf("allocator must have created the conversation row: %v", err)
	}
}
