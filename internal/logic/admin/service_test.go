package admin

import (
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/gateway"
	"golang-im-neo-system/internal/store"
	pb "golang-im-neo-system/proto"

	"google.golang.org/protobuf/proto"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := store.NewDB(store.DBConfig{Path: filepath.Join(t.TempDir(), "t.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	// Release the SQLite file handle so t.TempDir() cleanup succeeds on Windows.
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}

func newTestHub() *gateway.Hub {
	h := gateway.NewHub(zap.NewNop())
	go h.Run()
	return h
}

func waitForCount(t *testing.T, h *gateway.Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.Count() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("hub count: want %d, got %d", want, h.Count())
}

func seedUser(t *testing.T, db *gorm.DB, id string) *store.User {
	t.Helper()
	u := &store.User{ID: id, Username: "user_" + id, PasswordHash: "hash", Role: "user", Status: 1, TokenVersion: 1}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func seedSession(t *testing.T, db *gorm.DB, sid, uid, deviceClass string) {
	t.Helper()
	now := time.Now()
	s := &store.UserSession{
		ID: sid, UserID: uid, DeviceClass: deviceClass,
		RefreshTokenHash: "hash", TokenVersion: 1,
		ExpiresAt: now.Add(24 * time.Hour), LastActiveAt: now, CreatedAt: now,
	}
	if err := db.Create(s).Error; err != nil {
		t.Fatal(err)
	}
}

func recvTimeout(t *testing.T, ch chan []byte) []byte {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for message")
		return nil
	}
}

func TestBanUser(t *testing.T) {
	db := newTestDB(t)
	h := newTestHub()
	svc := NewAdminService(db, h, zap.NewNop())

	u := seedUser(t, db, "u_victim")
	victimTV := u.TokenVersion

	c1 := gateway.NewClient(h, nil, u.ID, "interactive", "s-desk", zap.NewNop())
	c2 := gateway.NewClient(h, nil, u.ID, "hardware", "s-hw", zap.NewNop())
	other := gateway.NewClient(h, nil, "u_other", "interactive", "s-other", zap.NewNop())
	h.Register(c1)
	h.Register(c2)
	h.Register(other)
	waitForCount(t, h, 3)

	if err := svc.BanUser(u.ID); err != nil {
		t.Fatalf("BanUser: %v", err)
	}

	var got store.User
	if err := db.Where("id = ?", u.ID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != 2 {
		t.Fatalf("status = %d, want 2", got.Status)
	}
	if got.TokenVersion != victimTV+1 {
		t.Fatalf("token_version = %d, want %d", got.TokenVersion, victimTV+1)
	}
	// All of the banned user's sessions drop; the other user stays.
	waitForCount(t, h, 1)
	if h.IsOnline(u.ID) {
		t.Fatalf("banned user should be offline")
	}
	if !h.IsOnline("u_other") {
		t.Fatalf("other user should stay online")
	}
}

func TestBanUserUnknown(t *testing.T) {
	db := newTestDB(t)
	svc := NewAdminService(db, newTestHub(), zap.NewNop())
	err := svc.BanUser("u_missing")
	if err == nil || CodeOf(err) != CodeUserNotFound {
		t.Fatalf("BanUser unknown: err=%v, want code 20001", err)
	}
	if HTTPStatusOf(CodeOf(err)) != 404 {
		t.Fatalf("20001 should map to 404")
	}
}

func TestKickSessionKeepsOtherSession(t *testing.T) {
	db := newTestDB(t)
	h := newTestHub()
	svc := NewAdminService(db, h, zap.NewNop())

	u := seedUser(t, db, "u_multi")
	seedSession(t, db, "s-desk", u.ID, "interactive")
	seedSession(t, db, "s-hw", u.ID, "hardware")

	desk := gateway.NewClient(h, nil, u.ID, "interactive", "s-desk", zap.NewNop())
	hw := gateway.NewClient(h, nil, u.ID, "hardware", "s-hw", zap.NewNop())
	h.Register(desk)
	h.Register(hw)
	waitForCount(t, h, 2)

	if err := svc.KickSession("s-desk"); err != nil {
		t.Fatalf("KickSession: %v", err)
	}

	var sess store.UserSession
	if err := db.Where("id = ?", "s-desk").First(&sess).Error; err != nil {
		t.Fatal(err)
	}
	if sess.IsRevoked != 1 {
		t.Fatalf("s-desk is_revoked = %d, want 1", sess.IsRevoked)
	}

	// ONLY the desktop session drops; the hardware session stays up.
	waitForCount(t, h, 1)
	if !h.IsOnline(u.ID) {
		t.Fatalf("user should stay online via hardware session")
	}
	payload := []byte("still-here")
	if !h.SendToUser(u.ID, payload) {
		t.Fatalf("SendToUser should reach the surviving session")
	}
	got := recvTimeout(t, hw.Send)
	if string(got) != string(payload) {
		t.Fatalf("hardware session got %q, want %q", got, payload)
	}
	select {
	case m, ok := <-desk.Send:
		if ok {
			t.Fatalf("kicked session should not receive messages, got %d bytes", len(m))
		}
		// closed channel: expected.
	default:
		// Channel close may race the async unregister path; Count already
		// proves removal, so an empty-but-open channel is acceptable here.
	}
}

func TestKickSessionUnknown(t *testing.T) {
	db := newTestDB(t)
	svc := NewAdminService(db, newTestHub(), zap.NewNop())
	err := svc.KickSession("s_missing")
	if err == nil || CodeOf(err) != CodeUserNotFound {
		t.Fatalf("KickSession unknown: err=%v, want code 20001", err)
	}
	if HTTPStatusOf(CodeOf(err)) != 404 {
		t.Fatalf("20001 should map to 404")
	}
}

func TestBroadcast(t *testing.T) {
	db := newTestDB(t)
	h := newTestHub()
	svc := NewAdminService(db, h, zap.NewNop())

	c1 := gateway.NewClient(h, nil, "u_1", "interactive", "s-1", zap.NewNop())
	c2 := gateway.NewClient(h, nil, "u_2", "interactive", "s-2", zap.NewNop())
	h.Register(c1)
	h.Register(c2)
	waitForCount(t, h, 2)

	if err := svc.Broadcast("hello all"); err != nil {
		t.Fatalf("Broadcast: %v", err)
	}
	for i, ch := range []chan []byte{c1.Send, c2.Send} {
		frame := recvTimeout(t, ch)
		var m pb.WsMessage
		if err := proto.Unmarshal(frame, &m); err != nil {
			t.Fatalf("client %d: unmarshal: %v", i, err)
		}
		if m.Type != pb.MsgType_SYSTEM_NOTICE {
			t.Fatalf("client %d: type = %v, want SYSTEM_NOTICE", i, m.Type)
		}
		if m.Content != "hello all" {
			t.Fatalf("client %d: content = %q", i, m.Content)
		}
		if m.Timestamp <= 0 {
			t.Fatalf("client %d: missing timestamp", i)
		}
	}

	if err := svc.Broadcast(""); err == nil || CodeOf(err) != CodeParamInvalid {
		t.Fatalf("Broadcast empty: err=%v, want code 10001", err)
	}
	if err := svc.Broadcast("   "); err == nil || CodeOf(err) != CodeParamInvalid {
		t.Fatalf("Broadcast blank: err=%v, want code 10001", err)
	}
}
