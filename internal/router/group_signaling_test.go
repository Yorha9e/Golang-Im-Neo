package router

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"gorm.io/gorm"

	pb "golang-im-neo-system/proto"

	"golang-im-neo-system/internal/gateway"
	"golang-im-neo-system/internal/logic/session"
	"golang-im-neo-system/internal/store"
)

// ---------- group fixtures ----------

func seedGroup(t *testing.T, db *gorm.DB, gid, owner string, members ...string) {
	t.Helper()
	if err := db.Create(&store.Group{
		ID: gid, Name: "g-" + gid, OwnerID: owner, MaxMembers: 500, Status: 1,
	}).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}
	for _, u := range append([]string{owner}, members...) {
		role, via := "member", "invite"
		if u == owner {
			role, via = "owner", "create"
		}
		if err := db.Create(&store.GroupMember{
			GroupID: gid, UserID: u, Role: role, JoinedVia: via,
		}).Error; err != nil {
			t.Fatalf("seed member %q: %v", u, err)
		}
	}
}

func setGroupMuted(t *testing.T, db *gorm.DB, gid, uid string, muted bool) {
	t.Helper()
	if err := db.Model(&store.GroupMember{}).
		Where("group_id = ? AND user_id = ?", gid, uid).
		Update("muted", muted).Error; err != nil {
		t.Fatalf("set muted: %v", err)
	}
}

func countRows(t *testing.T, db *gorm.DB, model interface{}) int64 {
	t.Helper()
	var n int64
	if err := db.Model(model).Count(&n).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

// ---------- 1. GROUP_CHAT happy path ----------

func TestGroupChatHappyPath(t *testing.T) {
	r, _, em, ps := newTestSetup(t)
	seedGroup(t, r.db, "g1", "gs_a", "gs_b", "gs_c")

	r.HandleInbound("gs_a", "sess-a", "interactive", mustFrame(t, &pb.WsMessage{
		Type: pb.MsgType_GROUP_CHAT, ToUid: "g1", Content: "hello group", StanzaId: "g-s1",
	}))

	if got := ps.numCalls(); got != 1 {
		t.Fatalf("persister calls = %d, want 1", got)
	}
	row := ps.lastCall()
	if row.CovID != "cov:grp:g1" {
		t.Fatalf("CovID = %q, want cov:grp:g1", row.CovID)
	}
	if row.ChatType != "groupchat" || row.ToUID != "g1" || row.FromUID != "gs_a" {
		t.Fatalf("row identity wrong: chatType=%q to=%q from=%q", row.ChatType, row.ToUID, row.FromUID)
	}
	if row.Seq != 1 || row.StanzaID != "g-s1" || row.Content != "hello group" {
		t.Fatalf("row payload wrong: %+v", row)
	}

	// Sender: exactly one ACK (seq>0) plus one echo frame (dual-signal).
	senderFrames := decodedFrames(t, em, "gs_a")
	acks := filterType(senderFrames, pb.MsgType_ACK)
	if len(acks) != 1 || acks[0].GetSeq() != 1 || acks[0].GetStanzaId() != "g-s1" {
		t.Fatalf("sender ACK wrong: %v", acks)
	}
	echoes := filterType(senderFrames, pb.MsgType_GROUP_CHAT)
	if len(echoes) != 1 {
		t.Fatalf("sender echo frames = %d, want 1", len(echoes))
	}
	echo := echoes[0]
	if echo.GetSeq() != 1 || echo.GetFromUid() != "gs_a" || echo.GetToUid() != "g1" ||
		echo.GetContent() != "hello group" || echo.GetStanzaId() != "g-s1" {
		t.Fatalf("echo frame wrong: %v", echo)
	}

	// Both members get the identical frame.
	for _, m := range []string{"gs_b", "gs_c"} {
		got := decodedFrames(t, em, m)
		if len(got) != 1 {
			t.Fatalf("member %s frames = %d, want 1", m, len(got))
		}
		f := got[0]
		if f.GetType() != pb.MsgType_GROUP_CHAT || f.GetSeq() != 1 ||
			f.GetFromUid() != "gs_a" || f.GetToUid() != "g1" ||
			f.GetContent() != "hello group" || f.GetStanzaId() != "g-s1" {
			t.Fatalf("member %s frame wrong: %v", m, f)
		}
	}

	// Group fan-out never touches the hall broadcast.
	if got := len(em.broadcastsCopy()); got != 0 {
		t.Fatalf("group chat must not broadcast, got %d", got)
	}
}

// ---------- 2. GROUP_CHAT dedup replay ----------

func TestGroupChatDedupReplay(t *testing.T) {
	r, _, em, ps := newTestSetup(t)
	seedGroup(t, r.db, "g1", "gs_a", "gs_b")

	first := mustFrame(t, &pb.WsMessage{
		Type: pb.MsgType_GROUP_CHAT, ToUid: "g1", Content: "hello", StanzaId: "g-dup",
	})
	r.HandleInbound("gs_a", "s", "interactive", first)
	r.HandleInbound("gs_a", "s", "interactive", mustFrame(t, &pb.WsMessage{
		Type: pb.MsgType_GROUP_CHAT, ToUid: "g1", Content: "hello-again", StanzaId: "g-dup",
	}))

	if got := ps.numCalls(); got != 1 {
		t.Fatalf("dedup replay persisted %d times, want exactly 1", got)
	}
	if got := len(decodedFrames(t, em, "gs_b")); got != 1 {
		t.Fatalf("dedup replay fanned out %d times, want exactly 1", got)
	}
	acks := filterType(decodedFrames(t, em, "gs_a"), pb.MsgType_ACK)
	if len(acks) != 2 || acks[0].GetSeq() != acks[1].GetSeq() || acks[0].GetSeq() != 1 {
		t.Fatalf("replay ACK must carry SAME seq 1, got %v", acks)
	}
	echoes := filterType(decodedFrames(t, em, "gs_a"), pb.MsgType_GROUP_CHAT)
	if len(echoes) != 1 {
		t.Fatalf("replay must not re-echo, echo frames = %d", len(echoes))
	}
}

// ---------- 3. GROUP_CHAT authorization ----------

func TestGroupChatAuthz(t *testing.T) {
	t.Run("non-member 40002, zero persist/push", func(t *testing.T) {
		r, _, em, ps := newTestSetup(t)
		seedGroup(t, r.db, "g1", "o1", "m1")

		r.HandleInbound("gx_out", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_GROUP_CHAT, ToUid: "g1", Content: "hi", StanzaId: "g-x1",
		}))

		if got := ps.numCalls(); got != 0 {
			t.Fatalf("non-member must not persist, calls = %d", got)
		}
		for _, u := range []string{"o1", "m1"} {
			if got := len(decodedFrames(t, em, u)); got != 0 {
				t.Fatalf("non-member must not push to %s, got %d", u, got)
			}
		}
		frames := decodedFrames(t, em, "gx_out")
		if len(frames) != 1 {
			t.Fatalf("sender must get exactly 1 notice, got %d", len(frames))
		}
		if code := noticeCode(t, frames[0]); code != CodeGroupNotMember {
			t.Fatalf("notice code = %d, want 40002", code)
		}
	})

	t.Run("muted 40005 then live unmute works, re-mute bites at once", func(t *testing.T) {
		r, db, em, ps := newTestSetup(t)
		seedGroup(t, db, "g1", "o1", "m1")
		setGroupMuted(t, db, "g1", "m1", true)

		r.HandleInbound("m1", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_GROUP_CHAT, ToUid: "g1", Content: "hi", StanzaId: "g-m1",
		}))
		if got := ps.numCalls(); got != 0 {
			t.Fatalf("muted member must not persist, calls = %d", got)
		}
		if got := len(decodedFrames(t, em, "o1")); got != 0 {
			t.Fatalf("muted member must not push, got %d", got)
		}
		frames := decodedFrames(t, em, "m1")
		if len(frames) != 1 {
			t.Fatalf("muted sender must get exactly 1 notice, got %d", len(frames))
		}
		if code := noticeCode(t, frames[0]); code != CodeGroupMuted {
			t.Fatalf("notice code = %d, want 40005", code)
		}

		// Live check: unmute takes effect with NO cache invalidation.
		setGroupMuted(t, db, "g1", "m1", false)
		r.HandleInbound("m1", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_GROUP_CHAT, ToUid: "g1", Content: "back", StanzaId: "g-m2",
		}))
		if got := ps.numCalls(); got != 1 {
			t.Fatalf("unmuted member must persist, calls = %d", got)
		}
		if got := len(decodedFrames(t, em, "o1")); got != 1 {
			t.Fatalf("unmuted member must fan out, pushes = %d", got)
		}

		// Re-mute bites on the very next frame (mute is never cached).
		setGroupMuted(t, db, "g1", "m1", true)
		r.HandleInbound("m1", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_GROUP_CHAT, ToUid: "g1", Content: "again", StanzaId: "g-m3",
		}))
		if got := ps.numCalls(); got != 1 {
			t.Fatalf("re-muted member must not persist again, calls = %d", got)
		}
		notices := filterType(decodedFrames(t, em, "m1"), pb.MsgType_SYSTEM_NOTICE)
		if len(notices) != 2 || noticeCode(t, notices[1]) != CodeGroupMuted {
			t.Fatalf("second mute notice wrong: %v", notices)
		}
	})

	t.Run("deny cached; InvalidateGroupMember re-allows", func(t *testing.T) {
		r, db, em, ps := newTestSetup(t)
		seedGroup(t, db, "g1", "o1", "m1")

		r.HandleInbound("gx_new", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_GROUP_CHAT, ToUid: "g1", Content: "hi", StanzaId: "g-n1",
		}))
		if got := ps.numCalls(); got != 0 {
			t.Fatalf("calls = %d, want 0", got)
		}

		// Join the group directly; the cached deny must still block.
		if err := db.Create(&store.GroupMember{
			GroupID: "g1", UserID: "gx_new", Role: "member", JoinedVia: "invite",
		}).Error; err != nil {
			t.Fatalf("add member: %v", err)
		}
		r.HandleInbound("gx_new", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_GROUP_CHAT, ToUid: "g1", Content: "hi", StanzaId: "g-n2",
		}))
		if got := ps.numCalls(); got != 0 {
			t.Fatalf("cached deny must still block, calls = %d", got)
		}

		// After invalidation the live membership verdict applies.
		r.InvalidateGroupMember("g1", "gx_new")
		r.HandleInbound("gx_new", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_GROUP_CHAT, ToUid: "g1", Content: "hi", StanzaId: "g-n3",
		}))
		if got := ps.numCalls(); got != 1 {
			t.Fatalf("post-invalidate must persist, calls = %d", got)
		}
		if got := len(decodedFrames(t, em, "o1")); got != 1 {
			t.Fatalf("post-invalidate must fan out, pushes = %d", got)
		}
	})
}

// ---------- 4. dual-variant group frame over a real Hub ----------

func recvClientFrame(t *testing.T, ch chan []byte) []byte {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for client frame")
		return nil
	}
}

func waitForHubCount(t *testing.T, h *gateway.Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.Count() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("hub count = %d, want %d", h.Count(), want)
}

func TestGroupChatDualVariantViaHub(t *testing.T) {
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
	ps := &fakePersister{}
	h := gateway.NewHub(zap.NewNop())
	go h.Run()
	r := New(h, ps, alloc, db, zap.NewNop())
	seedGroup(t, db, "g1", "gs_a", "gs_b", "gs_hw")

	ca := gateway.NewClient(h, nil, "gs_a", "interactive", "s-a", zap.NewNop())
	cb := gateway.NewClient(h, nil, "gs_b", "interactive", "s-b", zap.NewNop())
	chw := gateway.NewClient(h, nil, "gs_hw", "hardware", "s-hw", zap.NewNop())
	h.Register(ca)
	h.Register(cb)
	h.Register(chw)
	waitForHubCount(t, h, 3)

	long := strings.Repeat("A", 1000)
	r.HandleInbound("gs_a", "s-a", "interactive", mustFrame(t, &pb.WsMessage{
		Type: pb.MsgType_GROUP_CHAT, ToUid: "g1", Content: long, StanzaId: "g-big",
	}))

	if got := ps.numCalls(); got != 1 {
		t.Fatalf("persister calls = %d, want 1", got)
	}

	// Sender: ACK then full echo frame.
	ackRaw := recvClientFrame(t, ca.Send)
	if ack := mustDecode(t, ackRaw); ack.GetType() != pb.MsgType_ACK ||
		ack.GetSeq() != 1 || ack.GetStanzaId() != "g-big" {
		t.Fatalf("sender ACK wrong: %v", ack)
	}
	echoRaw := recvClientFrame(t, ca.Send)
	echo := mustDecode(t, echoRaw)
	if echo.GetType() != pb.MsgType_GROUP_CHAT || echo.GetSeq() != 1 ||
		echo.GetContent() != long || echo.GetFromUid() != "gs_a" || echo.GetToUid() != "g1" {
		t.Fatalf("sender echo wrong: seq=%d from=%q to=%q len=%d",
			echo.GetSeq(), echo.GetFromUid(), echo.GetToUid(), len(echo.GetContent()))
	}

	// Interactive member: the identical full frame (marshal-once).
	bRaw := recvClientFrame(t, cb.Send)
	if !bytes.Equal(bRaw, echoRaw) {
		t.Fatalf("interactive member must receive the identical frame bytes")
	}

	// Hardware member: truncated variant with suffix, valid UTF-8.
	hwRaw := recvClientFrame(t, chw.Send)
	hw := mustDecode(t, hwRaw)
	if hw.GetType() != pb.MsgType_GROUP_CHAT || hw.GetSeq() != 1 || hw.GetFromUid() != "gs_a" {
		t.Fatalf("hardware frame identity wrong: %v", hw)
	}
	if len(hw.GetContent()) > 256 {
		t.Fatalf("hardware content %d bytes exceeds 256", len(hw.GetContent()))
	}
	if !strings.HasSuffix(hw.GetContent(), gateway.TruncateSuffix) {
		t.Fatalf("hardware content must end with truncation suffix")
	}
	if !utf8.ValidString(hw.GetContent()) {
		t.Fatalf("hardware content must be valid UTF-8")
	}
}

// ---------- 6. SIGNALING happy path: memory-only bypass ----------

func TestSignalingHappyPathBypass(t *testing.T) {
	for _, typ := range []pb.MsgType{
		pb.MsgType_SIGNALING_OFFER,
		pb.MsgType_SIGNALING_ANSWER,
		pb.MsgType_SIGNALING_CANDIDATE,
	} {
		t.Run(typ.String(), func(t *testing.T) {
			r, db, em, ps := newTestSetup(t)
			seedFriends(t, db, "s_a", "s_b")
			msgBefore := countRows(t, db, &store.Message{})
			convBefore := countRows(t, db, &store.Conversation{})

			payload := []byte("v=0\r\no=- 123 456 IN IP4 127.0.0.1\r\ns=-\r\n")
			r.HandleInbound("s_a", "sess-1", "interactive", mustFrame(t, &pb.WsMessage{
				Type: typ, ToUid: "s_b", Content: "sdp-meta",
				Payload: payload, Extra: `{"sdp":"mline"}`, StanzaId: "sig-1",
			}))

			// ZERO DB pollution: no persist call, no message/conversation rows.
			if got := ps.numCalls(); got != 0 {
				t.Fatalf("signaling must never enqueue persist, calls = %d", got)
			}
			if got := countRows(t, db, &store.Message{}); got != msgBefore {
				t.Fatalf("messages rows %d -> %d, want unchanged", msgBefore, got)
			}
			if got := countRows(t, db, &store.Conversation{}); got != convBefore {
				t.Fatalf("conversations rows %d -> %d (Allocate watermark write!), want unchanged",
					convBefore, got)
			}

			// Peer receives the frame verbatim: type preserved, Seq 0.
			got := decodedFrames(t, em, "s_b")
			if len(got) != 1 {
				t.Fatalf("peer frames = %d, want 1", len(got))
			}
			f := got[0]
			if f.GetType() != typ || f.GetFromUid() != "s_a" || f.GetToUid() != "s_b" {
				t.Fatalf("signal frame identity wrong: %v", f)
			}
			if f.GetSeq() != 0 {
				t.Fatalf("signal Seq = %d, want 0 (no allocation)", f.GetSeq())
			}
			if !bytes.Equal(f.GetPayload(), payload) {
				t.Fatalf("signal payload mangled: %q", f.GetPayload())
			}
			if f.GetContent() != "sdp-meta" || f.GetExtra() != `{"sdp":"mline"}` ||
				f.GetStanzaId() != "sig-1" {
				t.Fatalf("signal fields must pass through verbatim: %v", f)
			}
			saneMillis(t, f.GetTimestamp())

			// No sender echo and no ACK for signaling.
			if frames := decodedFrames(t, em, "s_a"); len(frames) != 0 {
				t.Fatalf("signaling sender must get nothing, got %d frames", len(frames))
			}
		})
	}
}

// ---------- 7. SIGNALING deny paths ----------

func TestSignalingDenied(t *testing.T) {
	t.Run("one-direction-only friendship -> 30001", func(t *testing.T) {
		r, db, em, ps := newTestSetup(t)
		if err := db.Create(&store.Friendship{
			UserID: "s_a", FriendID: "s_b", InitiatorID: "s_a", Status: "accepted",
		}).Error; err != nil {
			t.Fatalf("seed one-way friendship: %v", err)
		}

		r.HandleInbound("s_a", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_SIGNALING_OFFER, ToUid: "s_b", StanzaId: "sig-x",
		}))

		if got := ps.numCalls(); got != 0 {
			t.Fatalf("denied signaling must not persist, calls = %d", got)
		}
		if got := len(decodedFrames(t, em, "s_b")); got != 0 {
			t.Fatalf("denied signaling must not push, got %d", got)
		}
		frames := decodedFrames(t, em, "s_a")
		if len(frames) != 1 {
			t.Fatalf("sender must get exactly 1 notice, got %d", len(frames))
		}
		if code := noticeCode(t, frames[0]); code != CodeFriendNotFound {
			t.Fatalf("notice code = %d, want 30001", code)
		}
	})

	t.Run("no friendship -> 30001", func(t *testing.T) {
		r, _, em, ps := newTestSetup(t)

		r.HandleInbound("s_a", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_SIGNALING_CANDIDATE, ToUid: "s_b", StanzaId: "sig-y",
		}))

		if got := ps.numCalls(); got != 0 {
			t.Fatalf("calls = %d, want 0", got)
		}
		if got := len(decodedFrames(t, em, "s_b")); got != 0 {
			t.Fatalf("pushes = %d, want 0", got)
		}
		frames := decodedFrames(t, em, "s_a")
		if len(frames) != 1 || noticeCode(t, frames[0]) != CodeFriendNotFound {
			t.Fatalf("sender must get exactly one 30001 notice, got %v", frames)
		}
	})

	t.Run("offline peer -> plain notice, nothing persisted", func(t *testing.T) {
		r, db, em, ps := newTestSetup(t)
		seedFriends(t, db, "s_a", "s_b")
		em.setOffline("s_b", true)

		r.HandleInbound("s_a", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_SIGNALING_OFFER, ToUid: "s_b", StanzaId: "sig-z",
		}))

		if got := ps.numCalls(); got != 0 {
			t.Fatalf("calls = %d, want 0", got)
		}
		frames := decodedFrames(t, em, "s_a")
		if len(frames) != 1 {
			t.Fatalf("sender must get exactly 1 notice, got %d", len(frames))
		}
		if frames[0].GetType() != pb.MsgType_SYSTEM_NOTICE || frames[0].GetExtra() != "" {
			t.Fatalf("offline notice must be plain (no code), got %v", frames[0])
		}
		if frames[0].GetContent() == "" {
			t.Fatal("offline notice must carry a human hint")
		}
	})
}

// ---------- missing recipients ----------

func TestGroupAndSignalingMissingRecipient(t *testing.T) {
	t.Run("group chat without group id", func(t *testing.T) {
		r, _, em, ps := newTestSetup(t)
		r.HandleInbound("u_a", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_GROUP_CHAT, Content: "hi", StanzaId: "g-0",
		}))
		if got := ps.numCalls(); got != 0 {
			t.Fatalf("calls = %d, want 0", got)
		}
		frames := decodedFrames(t, em, "u_a")
		if len(frames) != 1 || frames[0].GetType() != pb.MsgType_SYSTEM_NOTICE {
			t.Fatalf("sender must get exactly 1 plain notice, got %v", frames)
		}
	})

	t.Run("signaling without peer", func(t *testing.T) {
		r, _, em, ps := newTestSetup(t)
		r.HandleInbound("u_a", "s", "interactive", mustFrame(t, &pb.WsMessage{
			Type: pb.MsgType_SIGNALING_OFFER, StanzaId: "sig-0",
		}))
		if got := ps.numCalls(); got != 0 {
			t.Fatalf("calls = %d, want 0", got)
		}
		frames := decodedFrames(t, em, "u_a")
		if len(frames) != 1 || frames[0].GetType() != pb.MsgType_SYSTEM_NOTICE {
			t.Fatalf("sender must get exactly 1 plain notice, got %v", frames)
		}
	})
}
