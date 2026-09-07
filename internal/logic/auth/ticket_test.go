package auth

import (
	"strings"
	"testing"
	"time"
)

func TestTicketIssueFormat(t *testing.T) {
	ts := NewTicketService()
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
