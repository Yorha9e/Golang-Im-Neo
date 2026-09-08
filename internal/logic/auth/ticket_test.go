package auth

import (
	"strings"
	"testing"
	"time"
)

func TestTicketIssueFormat(t *testing.T) {
	ts := NewTicketService()
	defer ts.Stop()
	tkt := ts.Issue("u1", "alice", "user", "s1", "interactive")
	if !strings.HasPrefix(tkt, "tkt_") {
		t.Fatalf("ticket missing tkt_ prefix: %q", tkt)
	}
	hexPart := strings.TrimPrefix(tkt, "tkt_")
	if len(hexPart) != 32 {
		t.Fatalf("ticket hex part len=%d, want 32: %q", len(hexPart), tkt)
	}
	for _, r := range hexPart {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Fatalf("ticket not lowercase hex: %q", tkt)
		}
	}
}

func TestTicketSingleUse(t *testing.T) {
	ts := NewTicketService()
	defer ts.Stop()
	tkt := ts.Issue("u1", "alice", "user", "s1", "interactive")
	uid, uname, role, sid, dc, err := ts.Redeem(tkt)
	if err != nil {
		t.Fatalf("first Redeem: %v", err)
	}
	if uid != "u1" || uname != "alice" || role != "user" || sid != "s1" || dc != "interactive" {
		t.Fatalf("redeemed identity mismatch: %q %q %q %q %q", uid, uname, role, sid, dc)
	}
	// Second redeem of the same ticket must fail (single-use).
	if _, _, _, _, _, err := ts.Redeem(tkt); err == nil {
		t.Fatal("second Redeem must error")
	}
}

func TestTicketUnknown(t *testing.T) {
	ts := NewTicketService()
	defer ts.Stop()
	if _, _, _, _, _, err := ts.Redeem("tkt_" + strings.Repeat("0", 32)); err == nil {
		t.Fatal("unknown ticket must error")
	}
}

func TestTicketExpiry(t *testing.T) {
	ts := &TicketService{TTL: 50 * time.Millisecond}
	tkt := ts.Issue("u1", "alice", "user", "s1", "interactive")
	time.Sleep(200 * time.Millisecond)
	if _, _, _, _, _, err := ts.Redeem(tkt); err == nil {
		t.Fatal("expired ticket must error")
	}
}

func TestTicketSweeperPrunesExpired(t *testing.T) {
	ts := NewTicketService()
	defer ts.Stop()

	// Fresh ticket must survive the sweep.
	fresh := ts.Issue("u1", "alice", "user", "s1", "interactive")

	// Seed expired entries directly with past deadlines.
	past := time.Now().Add(-time.Minute)
	ts.items.Store("tkt_"+"deadbeefdeadbeefdeadbeefdeadbeef", ticketEntry{
		userID: "u9", username: "ghost", role: "user",
		sessionID: "s9", deviceClass: "interactive",
		expiresAt: past,
	})

	// A naturally-expired ticket: short TTL, then let it lapse.
	short := &TicketService{TTL: 20 * time.Millisecond}
	shortLived := short.Issue("u2", "bob", "user", "s2", "interactive")
	time.Sleep(50 * time.Millisecond)
	// Move it into the swept service so the sweeper sees an expired entry.
	if v, ok := short.items.Load(shortLived); ok {
		ts.items.Store(shortLived, v)
		short.items.Delete(shortLived)
	} else {
		t.Fatalf("short-lived ticket missing before sweep: %q", shortLived)
	}

	ts.sweep()

	if _, ok := ts.items.Load("tkt_" + "deadbeefdeadbeefdeadbeefdeadbeef"); ok {
		t.Fatal("sweeper must prune manually-expired ticket")
	}
	if _, ok := ts.items.Load(shortLived); ok {
		t.Fatal("sweeper must prune TTL-expired ticket")
	}
	if _, ok := ts.items.Load(fresh); !ok {
		t.Fatal("sweeper must keep unexpired ticket")
	}

	// The surviving ticket must still redeem with its identity intact.
	uid, uname, role, sid, dc, err := ts.Redeem(fresh)
	if err != nil {
		t.Fatalf("fresh ticket Redeem after sweep: %v", err)
	}
	if uid != "u1" || uname != "alice" || role != "user" || sid != "s1" || dc != "interactive" {
		t.Fatalf("redeemed identity mismatch after sweep: %q %q %q %q %q", uid, uname, role, sid, dc)
	}
}

func TestTicketStopIdempotent(t *testing.T) {
	ts := NewTicketService()

	// First Stop closes the channel; subsequent calls must not panic.
	ts.Stop()
	ts.Stop()

	select {
	case <-ts.stopChan:
	default:
		t.Fatal("stopChan should be closed after Stop")
	}

	// Zero-value service without a started goroutine must also tolerate Stop.
	var zero TicketService
	zero.Stop()
	zero.Stop()
}
