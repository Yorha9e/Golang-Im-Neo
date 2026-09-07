package gateway

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	improto "golang-im-neo-system/proto"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

func newTestHub() *Hub {
	h := NewHub(zap.NewNop())
	go h.Run()
	return h
}

func waitForCount(t *testing.T, h *Hub, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := h.Count(); got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitForCount: want %d, got %d", want, h.Count())
}

func newTestClient(h *Hub, userID, deviceClass, sessionID string) *Client {
	return NewClient(h, nil, userID, deviceClass, sessionID, zap.NewNop())
}

func tryRecv(ch chan []byte) ([]byte, bool) {
	select {
	case m := <-ch:
		return m, true
	default:
		return nil, false
	}
}

func recvTimeout(t *testing.T, ch chan []byte, timeout time.Duration) []byte {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for message")
		return nil
	}
}

func TestHubRegisterUnregisterAndCount(t *testing.T) {
	h := newTestHub()
	c1 := newTestClient(h, "alice", "interactive", "s-alice-1")
	c2 := newTestClient(h, "alice", "hardware", "s-alice-2")
	c3 := newTestClient(h, "bob", "interactive", "s-bob-1")

	h.Register(c1)
	h.Register(c2)
	h.Register(c3)
	waitForCount(t, h, 3, 2*time.Second)

	if !h.IsOnline("alice") || !h.IsOnline("bob") {
		t.Fatalf("IsOnline should be true for registered users")
	}
	if h.IsOnline("carol") {
		t.Fatalf("IsOnline should be false for unknown user")
	}

	h.Unregister(c1)
	waitForCount(t, h, 2, 2*time.Second)
	if !h.IsOnline("alice") {
		t.Fatalf("alice should still be online via second session")
	}

	h.Unregister(c2)
	h.Unregister(c3)
	waitForCount(t, h, 0, 2*time.Second)
	if h.IsOnline("alice") || h.IsOnline("bob") {
		t.Fatalf("all users should be offline after unregister")
	}
	// Double unregister must not panic or change count.
	h.Unregister(c3)
	time.Sleep(50 * time.Millisecond)
	if got := h.Count(); got != 0 {
		t.Fatalf("double unregister changed count: %d", got)
	}
}

func TestShardSpread(t *testing.T) {
	h := NewHub(zap.NewNop())
	seen := map[*bucket]bool{}
	for i := 0; i < 200; i++ {
		uid := fmt.Sprintf("user-%03d", i)
		seen[h.shardFor(uid)] = true
	}
	if len(seen) < 8 {
		t.Fatalf("shard spread too narrow: %d buckets for 200 users, want >= 8", len(seen))
	}
	if ShardCount != 32 {
		t.Fatalf("ShardCount = %d, want 32", ShardCount)
	}
}

func TestSendToUser(t *testing.T) {
	h := newTestHub()
	a1 := newTestClient(h, "alice", "interactive", "s-a1")
	a2 := newTestClient(h, "alice", "interactive", "s-a2")
	b1 := newTestClient(h, "bob", "interactive", "s-b1")
	h.Register(a1)
	h.Register(a2)
	h.Register(b1)
	waitForCount(t, h, 3, 2*time.Second)

	msg := []byte("hello-alice")
	if !h.SendToUser("alice", msg) {
		t.Fatalf("SendToUser should return true when delivered")
	}
	g1 := recvTimeout(t, a1.Send, time.Second)
	g2 := recvTimeout(t, a2.Send, time.Second)
	if !bytes.Equal(g1, msg) || !bytes.Equal(g2, msg) {
		t.Fatalf("alice sessions should receive exact payload")
	}
	if m, ok := tryRecv(b1.Send); ok {
		t.Fatalf("bob should receive nothing, got %q", m)
	}
	if h.SendToUser("nobody", msg) {
		t.Fatalf("SendToUser unknown user should return false")
	}
}

func TestBroadcast(t *testing.T) {
	h := newTestHub()
	clients := []*Client{
		newTestClient(h, "u1", "interactive", "s-1"),
		newTestClient(h, "u2", "interactive", "s-2"),
		newTestClient(h, "u3", "interactive", "s-3"),
	}
	for _, c := range clients {
		h.Register(c)
	}
	waitForCount(t, h, 3, 2*time.Second)

	msg := []byte("marshal-once-payload")
	h.Broadcast(msg)
	for i, c := range clients {
		got := recvTimeout(t, c.Send, time.Second)
		if !bytes.Equal(got, msg) {
			t.Fatalf("client %d should receive identical payload (marshal-once)", i)
		}
	}
}

func TestBackpressureBreaker(t *testing.T) {
	h := newTestHub()
	c := newTestClient(h, "slow", "interactive", "s-slow")
	h.Register(c)
	waitForCount(t, h, 1, 2*time.Second)

	// Fill the 256-frame buffer without draining.
	for i := 0; i < SendBufferSize; i++ {
		if !c.Enqueue([]byte{byte(i)}) {
			t.Fatalf("enqueue %d should succeed (buffer not full yet)", i)
		}
	}
	if c.Enqueue([]byte("overflow")) {
		t.Fatalf("Enqueue on full buffer should return false (breaker)")
	}

	// Kick path: a send to the slow client trips the breaker and unregisters it.
	h.SendToUser("slow", []byte("kick-me"))
	waitForCount(t, h, 0, 2*time.Second)
}

func TestHardwareTruncation(t *testing.T) {
	h := newTestHub()
	hw := newTestClient(h, "eve", "hardware", "s-hw")
	inter := newTestClient(h, "mallory", "interactive", "s-inter")
	h.Register(hw)
	h.Register(inter)
	waitForCount(t, h, 2, 2*time.Second)

	long := strings.Repeat("A", 1000)
	m := &improto.WsMessage{Type: improto.MsgType_CHAT, FromUid: "x", Content: long}
	frame, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(frame) <= 256 {
		t.Fatalf("test frame should exceed 256 bytes, got %d", len(frame))
	}

	h.Broadcast(frame)

	hwFrame := recvTimeout(t, hw.Send, time.Second)
	interFrame := recvTimeout(t, inter.Send, time.Second)

	// Interactive keeps the original bytes (marshal-once).
	if !bytes.Equal(interFrame, frame) {
		t.Fatalf("interactive client must receive original bytes")
	}
	var hwMsg improto.WsMessage
	if err := proto.Unmarshal(hwFrame, &hwMsg); err != nil {
		t.Fatalf("hardware frame should still be valid proto: %v", err)
	}
	if len(hwMsg.Content) > 256 {
		t.Fatalf("hardware content %d bytes exceeds 256", len(hwMsg.Content))
	}
	if !strings.HasSuffix(hwMsg.Content, TruncateSuffix) {
		t.Fatalf("hardware content should end with truncation suffix, got tail %q", tail(hwMsg.Content, 30))
	}
	if !utf8.ValidString(hwMsg.Content) {
		t.Fatalf("hardware content must be valid UTF-8")
	}
}

func TestHardwarePassthroughSmallFrame(t *testing.T) {
	h := newTestHub()
	hw := newTestClient(h, "tiny-hw", "hardware", "s-hw")
	h.Register(hw)
	waitForCount(t, h, 1, 2*time.Second)

	m := &improto.WsMessage{Type: improto.MsgType_CHAT, Content: "hi"}
	frame, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(frame) > 256 {
		t.Fatalf("small test frame should be <= 256, got %d", len(frame))
	}
	h.Broadcast(frame)
	got := recvTimeout(t, hw.Send, time.Second)
	if !bytes.Equal(got, frame) {
		t.Fatalf("small frames must pass through untouched")
	}
}

func TestHardwareTruncationRuneBoundary(t *testing.T) {
	h := newTestHub()
	hw := newTestClient(h, "cn", "hardware", "s-hw")
	h.Register(hw)
	waitForCount(t, h, 1, 2*time.Second)

	longCN := strings.Repeat("长", 300) // 900 bytes of 3-byte runes
	m := &improto.WsMessage{Type: improto.MsgType_CHAT, Content: longCN}
	frame, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	h.Broadcast(frame)
	got := recvTimeout(t, hw.Send, time.Second)
	var decoded improto.WsMessage
	if err := proto.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !utf8.ValidString(decoded.Content) {
		t.Fatalf("truncated content must be valid UTF-8 (rune boundary)")
	}
	if len(decoded.Content) > 256 {
		t.Fatalf("truncated content %d bytes exceeds 256", len(decoded.Content))
	}
	if !strings.HasSuffix(decoded.Content, TruncateSuffix) {
		t.Fatalf("truncated content must end with suffix")
	}
}

func TestSendToUserHardwareTruncation(t *testing.T) {
	h := newTestHub()
	hw1 := newTestClient(h, "victim", "hardware", "s-hw1")
	hw2 := newTestClient(h, "victim", "hardware", "s-hw2")
	other := newTestClient(h, "other", "interactive", "s-o")
	h.Register(hw1)
	h.Register(hw2)
	h.Register(other)
	waitForCount(t, h, 3, 2*time.Second)

	long := strings.Repeat("B", 800)
	m := &improto.WsMessage{Type: improto.MsgType_PRIVATE_CHAT, Content: long}
	frame, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !h.SendToUser("victim", frame) {
		t.Fatalf("SendToUser should succeed")
	}
	for _, hw := range []*Client{hw1, hw2} {
		got := recvTimeout(t, hw.Send, time.Second)
		var decoded improto.WsMessage
		if err := proto.Unmarshal(got, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(decoded.Content) > 256 || !strings.HasSuffix(decoded.Content, TruncateSuffix) {
			t.Fatalf("hardware SendToUser variant must be truncated with suffix")
		}
	}
	if m, ok := tryRecv(other.Send); ok {
		t.Fatalf("other user should receive nothing, got %d bytes", len(m))
	}
}

func TestKickUser(t *testing.T) {
	h := newTestHub()
	a1 := newTestClient(h, "alice", "interactive", "s-a1")
	a2 := newTestClient(h, "alice", "hardware", "s-a2")
	b1 := newTestClient(h, "bob", "interactive", "s-b1")
	h.Register(a1)
	h.Register(a2)
	h.Register(b1)
	waitForCount(t, h, 3, 2*time.Second)

	h.KickUser("alice", "test-kick")
	waitForCount(t, h, 1, 2*time.Second)
	if h.IsOnline("alice") {
		t.Fatalf("alice should be offline after KickUser")
	}
	if !h.IsOnline("bob") {
		t.Fatalf("bob should remain online after kicking alice")
	}
}

func TestClientCountAlias(t *testing.T) {
	h := newTestHub()
	if h.ClientCount() != 0 {
		t.Fatalf("fresh hub ClientCount should be 0")
	}
	c := newTestClient(h, "z", "interactive", "s-z")
	h.Register(c)
	waitForCount(t, h, 1, 2*time.Second)
	if h.ClientCount() != h.Count() {
		t.Fatalf("ClientCount should equal Count")
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
