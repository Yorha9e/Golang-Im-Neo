package handshake_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"golang-im-neo-system/internal/gateway"
	"golang-im-neo-system/internal/handshake"
)

type fakeVerifier struct {
	mu      sync.Mutex
	tickets map[string]fakeIdentity
	calls   int
}

type fakeIdentity struct {
	userID, username, role, sessionID, deviceClass string
}

func newFakeVerifier() *fakeVerifier {
	return &fakeVerifier{tickets: map[string]fakeIdentity{}}
}

func (f *fakeVerifier) add(ticket string, id fakeIdentity) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tickets[ticket] = id
}

func (f *fakeVerifier) Redeem(ticket string) (string, string, string, string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	id, ok := f.tickets[ticket]
	if !ok {
		return "", "", "", "", "", errInvalidTicket
	}
	return id.userID, id.username, id.role, id.sessionID, id.deviceClass, nil
}

type ticketErr string

func (e ticketErr) Error() string { return string(e) }

var errInvalidTicket ticketErr = "invalid ticket"

type fakeInbound struct {
	mu     sync.Mutex
	calls  []inboundCall
	notify chan inboundCall
}

type inboundCall struct {
	userID, sessionID, deviceClass string
	frame                          []byte
}

func newFakeInbound() *fakeInbound {
	return &fakeInbound{notify: make(chan inboundCall, 16)}
}

func (f *fakeInbound) HandleInbound(userID, sessionID, deviceClass string, frame []byte) {
	cp := append([]byte(nil), frame...)
	call := inboundCall{userID, sessionID, deviceClass, cp}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	select {
	case f.notify <- call:
	default:
	}
}

func setupServer(t *testing.T, verifier handshake.TicketVerifier, checkOrigin func(r *http.Request) bool) (*gateway.Hub, *fakeInbound, *httptest.Server) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hub := gateway.NewHub(zap.NewNop())
	go hub.Run()
	inbound := newFakeInbound()
	hub.SetInboundHandler(inbound)
	h := handshake.NewHandler(hub, verifier, zap.NewNop(), checkOrigin)
	router := gin.New()
	router.GET("/ws", h.ServeWS)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return hub, inbound, srv
}

func wsURL(srv *httptest.Server, ticket string) string {
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	if ticket != "" {
		u += "?ticket=" + ticket
	}
	return u
}

func waitForHubCount(t *testing.T, hub *gateway.Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.Count() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("hub Count = %d, want %d", hub.Count(), want)
}

func TestMissingTicket401(t *testing.T) {
	verifier := newFakeVerifier()
	_, _, srv := setupServer(t, verifier, nil)

	resp, err := http.Get(srv.URL + "/ws")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["code"] != float64(10002) || body["msg"] != "missing ticket" {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestInvalidTicket401(t *testing.T) {
	verifier := newFakeVerifier()
	_, _, srv := setupServer(t, verifier, nil)

	resp, err := http.Get(srv.URL + "/ws?ticket=bad-ticket")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["code"] != float64(10002) || body["msg"] != "invalid or expired ticket" {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestValidTicketUpgradeAndInbound(t *testing.T) {
	verifier := newFakeVerifier()
	verifier.add("good-ticket", fakeIdentity{
		userID: "u-alice", username: "alice", role: "user",
		sessionID: "s-1", deviceClass: "interactive",
	})
	hub, inbound, srv := setupServer(t, verifier, nil)

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, resp, err := dialer.Dial(wsURL(srv, "good-ticket"), nil)
	if err != nil {
		t.Fatalf("dial failed: %v (resp=%v)", err, resp)
	}
	defer conn.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status = %d, want 101", resp.StatusCode)
	}
	waitForHubCount(t, hub, 1)
	if !hub.IsOnline("u-alice") {
		t.Fatalf("u-alice should be online after upgrade")
	}

	frame := []byte("hello-router-frame")
	if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case call := <-inbound.notify:
		if call.userID != "u-alice" || call.sessionID != "s-1" || call.deviceClass != "interactive" {
			t.Fatalf("inbound identity mismatch: %+v", call)
		}
		if string(call.frame) != string(frame) {
			t.Fatalf("inbound frame mismatch: got %q want %q", call.frame, frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for inbound delivery")
	}
}

func TestDisallowedOrigin403(t *testing.T) {
	verifier := newFakeVerifier()
	verifier.add("good-ticket", fakeIdentity{
		userID: "u-bob", username: "bob", role: "user",
		sessionID: "s-2", deviceClass: "interactive",
	})
	_, _, srv := setupServer(t, verifier, nil)

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	header := http.Header{"Origin": {"http://evil.com"}}
	_, resp, err := dialer.Dial(wsURL(srv, "good-ticket"), header)
	if err == nil {
		t.Fatalf("dial with evil origin should fail")
	}
	if resp == nil {
		t.Fatalf("expected HTTP response on failed upgrade")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for disallowed origin", resp.StatusCode)
	}
}

func TestAllowAllOriginsHelper(t *testing.T) {
	verifier := newFakeVerifier()
	verifier.add("dev-ticket", fakeIdentity{
		userID: "u-dev", username: "dev", role: "user",
		sessionID: "s-dev", deviceClass: "interactive",
	})
	hub, _, srv := setupServer(t, verifier, handshake.AllowAllOrigins)

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	header := http.Header{"Origin": {"http://evil.com"}}
	conn, _, err := dialer.Dial(wsURL(srv, "dev-ticket"), header)
	if err != nil {
		t.Fatalf("AllowAllOrigins should permit evil origin: %v", err)
	}
	defer conn.Close()
	waitForHubCount(t, hub, 1)
}
