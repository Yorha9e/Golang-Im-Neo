// Stage-3 DoD proof (MISSION M3): multi-device coexistence — one user online
// on both device classes (interactive desktop + hardware ESP32) with no
// cross-class kick, broadcast + private delivery to both sessions, and the
// hardware 256-byte truncation adapter leaving interactive sessions untouched.
package app_test

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	pb "golang-im-neo-system/proto"

	"golang-im-neo-system/internal/app"
	"golang-im-neo-system/internal/gateway"
)

// s3Register registers a user and returns the new user id.
func s3Register(t *testing.T, base, user string) string {
	t.Helper()
	data := e2ePOST(t, base, "/api/v1/auth/register",
		map[string]string{"username": user, "password": e2ePass}, "")
	var reg struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(data, &reg); err != nil || reg.UserID == "" {
		t.Fatalf("register %s: bad data %s", user, short(data))
	}
	return reg.UserID
}

// s3BigContent builds ~1000 bytes of valid UTF-8 with multibyte runes
// straddling any 256-byte cut: 100 ASCII bytes followed by 300 CJK runes
// (3 bytes each). The 236-byte hardware prefix budget therefore ends
// mid-rune and must back off to a rune boundary.
func s3BigContent() string {
	return strings.Repeat("a", 100) + strings.Repeat("中", 300)
}

func TestStage3MultiDeviceCoexistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "e2e-s3.db")
	cfg := e2eConfig(t, dbPath)

	a, err := app.Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	srv := httptest.NewServer(a.Engine)
	t.Cleanup(srv.Close)
	base := srv.URL

	// ---- 1. Cross-class coexistence: same user on interactive + hardware ----
	aUID := s3Register(t, base, "s3alice")
	desk := s2LoginFull(t, base, "s3alice", e2ePass, "interactive", "desktop-s3")
	hw := s2LoginFull(t, base, "s3alice", e2ePass, "hardware", "esp32-s3")
	if desk.userID != aUID || hw.userID != aUID {
		t.Fatalf("login uid mismatch: reg=%s desk=%s hw=%s", aUID, desk.userID, hw.userID)
	}
	if desk.sessionID == hw.sessionID {
		t.Fatalf("distinct device classes must yield distinct sessions")
	}
	wDesk := e2eDial(t, base, e2eTicket(t, base, desk.at))
	wHW := e2eDial(t, base, e2eTicket(t, base, hw.at))
	waitHubCount(t, a, 2)

	// ---- 2. Broadcast to both sessions ----
	bob := e2eRegisterLogin(t, base, "s3bob")
	wB := e2eDial(t, base, e2eTicket(t, base, bob.at))
	waitHubCount(t, a, 3)

	sBcast := uuid.NewString()
	bcastContent := "s3-hall-hello-" + sBcast
	wB.send(t, &pb.WsMessage{Type: pb.MsgType_CHAT, Content: bcastContent, StanzaId: sBcast})
	ackB := wB.await(t, "bob ACK bcast", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_ACK && m.GetStanzaId() == sBcast
	})
	if ackB.GetSeq() <= 0 {
		t.Fatalf("bcast ACK bad seq %d", ackB.GetSeq())
	}
	gotDesk := wDesk.await(t, "desktop CHAT bcast", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_CHAT && m.GetStanzaId() == sBcast
	})
	gotHW := wHW.await(t, "esp32 CHAT bcast", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_CHAT && m.GetStanzaId() == sBcast
	})
	for name, got := range map[string]*pb.WsMessage{"desktop": gotDesk, "esp32": gotHW} {
		if got.GetFromUid() != bob.userID {
			t.Fatalf("%s bcast from=%q want %q", name, got.GetFromUid(), bob.userID)
		}
		if got.GetContent() != bcastContent {
			t.Fatalf("%s bcast content=%q want %q", name, got.GetContent(), bcastContent)
		}
		if got.GetSeq() != ackB.GetSeq() {
			t.Fatalf("%s bcast seq=%d want ACK seq=%d", name, got.GetSeq(), ackB.GetSeq())
		}
	}
	if gotDesk.GetContent() != gotHW.GetContent() {
		t.Fatalf("small broadcast must reach both classes identically")
	}
	// Consume the sender's own broadcast echo so it cannot leak into later awaits.
	gotBEcho := wB.await(t, "bob own CHAT echo", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_CHAT && m.GetStanzaId() == sBcast
	})
	if gotBEcho.GetContent() != bcastContent {
		t.Fatalf("sender echo content=%q want %q", gotBEcho.GetContent(), bcastContent)
	}
	// Desktop received a post-hardware-login message: it was NOT kicked.

	// ---- 3. Private chat to both sessions ----
	seedFriends(t, a, aUID, bob.userID)
	sPriv := uuid.NewString()
	privContent := "s3-private-hello-" + sPriv
	wB.send(t, &pb.WsMessage{Type: pb.MsgType_PRIVATE_CHAT, ToUid: aUID, Content: privContent, StanzaId: sPriv})
	ackP := wB.await(t, "bob ACK private", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_ACK && m.GetStanzaId() == sPriv
	})
	if ackP.GetSeq() <= 0 {
		t.Fatalf("private ACK bad seq %d", ackP.GetSeq())
	}
	gotDeskP := wDesk.await(t, "desktop PRIVATE", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_PRIVATE_CHAT && m.GetStanzaId() == sPriv
	})
	gotHWP := wHW.await(t, "esp32 PRIVATE", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_PRIVATE_CHAT && m.GetStanzaId() == sPriv
	})
	for name, got := range map[string]*pb.WsMessage{"desktop": gotDeskP, "esp32": gotHWP} {
		if got.GetFromUid() != bob.userID {
			t.Fatalf("%s private from=%q want %q", name, got.GetFromUid(), bob.userID)
		}
		if got.GetContent() != privContent {
			t.Fatalf("%s private content=%q want %q", name, got.GetContent(), privContent)
		}
		if got.GetSeq() != ackP.GetSeq() {
			t.Fatalf("%s private seq=%d want ACK seq=%d", name, got.GetSeq(), ackP.GetSeq())
		}
	}

	// ---- 4. Hardware truncation, interactive untouched, zero panic ----
	longContent := s3BigContent()
	if len(longContent) <= 256 {
		t.Fatalf("long content setup broken: %d bytes, want >256", len(longContent))
	}
	if !utf8.ValidString(longContent) {
		t.Fatalf("long content setup broken: invalid UTF-8")
	}
	sLong := uuid.NewString()
	wB.send(t, &pb.WsMessage{Type: pb.MsgType_CHAT, Content: longContent, StanzaId: sLong})
	wB.await(t, "bob ACK long", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_ACK && m.GetStanzaId() == sLong
	})
	gotDeskL := wDesk.await(t, "desktop CHAT long", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_CHAT && m.GetStanzaId() == sLong
	})
	if gotDeskL.GetContent() != longContent {
		t.Fatalf("desktop must receive FULL content: got %d bytes want %d", len(gotDeskL.GetContent()), len(longContent))
	}
	gotHWL := wHW.await(t, "esp32 CHAT long", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_CHAT && m.GetStanzaId() == sLong
	})
	hwContent := gotHWL.GetContent()
	if !strings.HasSuffix(hwContent, gateway.TruncateSuffix) {
		t.Fatalf("esp32 content must end with TruncateSuffix %q, got tail %q (len=%d)",
			gateway.TruncateSuffix, hwContent[max(0, len(hwContent)-32):], len(hwContent))
	}
	if len(hwContent) > gateway.HardwareContentBudget {
		t.Fatalf("esp32 content %d bytes exceeds %d-byte budget", len(hwContent), gateway.HardwareContentBudget)
	}
	if !utf8.ValidString(hwContent) {
		t.Fatalf("esp32 truncated content must stay valid UTF-8")
	}
	if hwContent == longContent {
		t.Fatalf("esp32 content must differ from the full original")
	}
	if gotHWL.GetSeq() != gotDeskL.GetSeq() {
		t.Fatalf("truncated variant must keep the server seq: hw=%d desk=%d", gotHWL.GetSeq(), gotDeskL.GetSeq())
	}
	if gotHWL.GetFromUid() != bob.userID || gotDeskL.GetFromUid() != bob.userID {
		t.Fatalf("long bcast from mismatch: desk=%q hw=%q want %q", gotDeskL.GetFromUid(), gotHWL.GetFromUid(), bob.userID)
	}
	// Sender (interactive) echo of the long message is full-length.
	gotBL := wB.await(t, "bob own long echo", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_CHAT && m.GetStanzaId() == sLong
	})
	if gotBL.GetContent() != longContent {
		t.Fatalf("sender echo must be full content: got %d bytes want %d", len(gotBL.GetContent()), len(longContent))
	}

	// Liveness after the truncation path: one more small broadcast must land
	// intact on both sessions (no panic, no killed session).
	sAfter := uuid.NewString()
	afterContent := "s3-alive-after-trunc-" + sAfter
	wB.send(t, &pb.WsMessage{Type: pb.MsgType_CHAT, Content: afterContent, StanzaId: sAfter})
	wB.await(t, "bob ACK after", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_ACK && m.GetStanzaId() == sAfter
	})
	gotDeskA := wDesk.await(t, "desktop CHAT after", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_CHAT && m.GetStanzaId() == sAfter
	})
	gotHWA := wHW.await(t, "esp32 CHAT after", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_CHAT && m.GetStanzaId() == sAfter
	})
	if gotDeskA.GetContent() != afterContent || gotHWA.GetContent() != afterContent {
		t.Fatalf("post-truncation delivery broken: desk=%q hw=%q want %q",
			gotDeskA.GetContent(), gotHWA.GetContent(), afterContent)
	}
	wB.await(t, "bob own after echo", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_CHAT && m.GetStanzaId() == sAfter
	})
	waitHubCount(t, a, 3)
}
