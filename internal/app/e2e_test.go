// Stage-1 DoD proof (MISSION M5): full app on 127.0.0.1:0 with two real WS
// clients covering public broadcast ACK+delivery, private delivery behind a
// DB-seeded friendship, 30001 denial, stanza dedup replay, and
// kill/restart-on-same-DB recovery returning the ORIGINAL seq.
package app_test

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/proto"

	pb "golang-im-neo-system/proto"

	"golang-im-neo-system/internal/app"
	"golang-im-neo-system/internal/config"
	"golang-im-neo-system/internal/store"
)

const (
	e2ePass    = "password123"
	e2eTimeout = 5 * time.Second
	// negWindow bounds "receives nothing" assertions (mission cap: <=200ms).
	negWindow = 200 * time.Millisecond
)

func e2eConfig(t *testing.T, dbPath string) *config.Config {
	t.Helper()
	var rb [16]byte
	if _, err := rand.Read(rb[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return &config.Config{
		Server: config.ServerConfig{
			Host: "127.0.0.1", Port: 8080,
			ReadTimeout: "30s", WriteTimeout: "30s", Mode: "test",
		},
		Security: config.SecurityConfig{
			JWTSecret:        "e2e-" + hex.EncodeToString(rb[:]),
			JWTAccessExpire:  "2h",
			JWTRefreshExpire: "336h",
			BcryptCost:       bcrypt.MinCost, // speed; prod uses 10
		},
		Database: config.DatabaseConfig{
			Path: dbPath, MaxOpenConns: 1, MaxIdleConns: 1, BusyTimeout: "5000",
		},
		Admin: config.AdminConfig{Username: "superadmin", Password: "SuperAdmin123!"},
	}
}

// ---------- HTTP helpers ----------

type e2eEnv struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

var e2eHTTP = &http.Client{Timeout: 10 * time.Second}

func e2ePOST(t *testing.T, base, path string, body interface{}, token string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("build req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e2eHTTP.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var env e2eEnv
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("POST %s: bad envelope %q: %v", path, short(raw), err)
	}
	if env.Code != 0 {
		t.Fatalf("POST %s: code=%d msg=%q http=%d", path, env.Code, env.Msg, resp.StatusCode)
	}
	return env.Data
}

func short(b []byte) string {
	if len(b) > 160 {
		return string(b[:160]) + "..."
	}
	return string(b)
}

type e2eIdent struct {
	userID, at string
}

func e2eRegisterLogin(t *testing.T, base, user string) e2eIdent {
	t.Helper()
	data := e2ePOST(t, base, "/api/v1/auth/register",
		map[string]string{"username": user, "password": e2ePass}, "")
	var reg struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(data, &reg); err != nil || reg.UserID == "" {
		t.Fatalf("register %s: bad data %s", user, short(data))
	}
	data = e2ePOST(t, base, "/api/v1/auth/login", map[string]string{
		"username": user, "password": e2ePass,
		"device_class": "interactive", "device_name": "e2e",
	}, "")
	var login struct {
		AccessToken string `json:"access_token"`
		UserID      string `json:"user_id"`
		SessionID   string `json:"session_id"`
	}
	if err := json.Unmarshal(data, &login); err != nil || login.AccessToken == "" {
		t.Fatalf("login %s: bad data %s", user, short(data))
	}
	if login.UserID != reg.UserID {
		t.Fatalf("login uid %q != registered %q", login.UserID, reg.UserID)
	}
	return e2eIdent{userID: reg.UserID, at: login.AccessToken}
}

func e2eTicket(t *testing.T, base, at string) string {
	t.Helper()
	data := e2ePOST(t, base, "/api/v1/auth/ticket", map[string]string{}, at)
	var out struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.Ticket == "" {
		t.Fatalf("ticket: bad data %s", short(data))
	}
	return out.Ticket
}

// ---------- WS helpers ----------

type e2eWS struct {
	conn *websocket.Conn
	ch   chan *pb.WsMessage
}

func e2eDial(t *testing.T, base, ticket string) *e2eWS {
	t.Helper()
	u := "ws" + strings.TrimPrefix(base, "http") + "/ws?ticket=" + url.QueryEscape(ticket)
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: e2eTimeout}).Dial(u, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	w := &e2eWS{conn: conn, ch: make(chan *pb.WsMessage, 256)}
	go func() {
		defer close(w.ch)
		for {
			typ, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if typ != websocket.BinaryMessage {
				continue
			}
			m := &pb.WsMessage{}
			if err := proto.Unmarshal(data, m); err != nil {
				continue
			}
			w.ch <- m
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return w
}

func (w *e2eWS) send(t *testing.T, m *pb.WsMessage) {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := w.conn.WriteMessage(websocket.BinaryMessage, b); err != nil {
		t.Fatalf("ws write: %v", err)
	}
}

func (w *e2eWS) await(t *testing.T, what string, pred func(*pb.WsMessage) bool) *pb.WsMessage {
	t.Helper()
	deadline := time.Now().Add(e2eTimeout)
	for {
		rest := time.Until(deadline)
		if rest <= 0 {
			t.Fatalf("timeout waiting for %s", what)
		}
		select {
		case m, ok := <-w.ch:
			if !ok {
				t.Fatalf("conn closed while waiting for %s", what)
			}
			if pred(m) {
				return m
			}
		case <-time.After(rest):
			t.Fatalf("timeout waiting for %s", what)
		}
	}
}

// drain collects every frame arriving within d; empty means "got nothing".
func (w *e2eWS) drain(d time.Duration) []*pb.WsMessage {
	var out []*pb.WsMessage
	deadline := time.Now().Add(d)
	for {
		rest := time.Until(deadline)
		if rest <= 0 {
			return out
		}
		select {
		case m, ok := <-w.ch:
			if !ok {
				return out
			}
			out = append(out, m)
		case <-time.After(rest):
			return out
		}
	}
}

func noticeCode(t *testing.T, m *pb.WsMessage) int {
	t.Helper()
	var e struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal([]byte(m.GetExtra()), &e); err != nil {
		t.Fatalf("notice Extra not coded JSON %q: %v", m.GetExtra(), err)
	}
	return e.Code
}

func waitHubCount(t *testing.T, a *app.App, want int) {
	t.Helper()
	deadline := time.Now().Add(e2eTimeout)
	for time.Now().Before(deadline) {
		if a.Hub.Count() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("hub Count = %d, want %d", a.Hub.Count(), want)
}

func seedFriends(t *testing.T, a *app.App, x, y string) {
	t.Helper()
	for _, p := range [][2]string{{x, y}, {y, x}} {
		if err := a.DB.Create(&store.Friendship{
			UserID: p[0], FriendID: p[1], InitiatorID: p[0], Status: "accepted",
		}).Error; err != nil {
			t.Fatalf("seed friendship %v: %v", p, err)
		}
	}
}

// ---------- the DoD test ----------

func TestStage1EndToEnd(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "e2e.db")
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

	// 1. alice + bob register/login/ticket/connect; hub sees 2 conns.
	alice := e2eRegisterLogin(t, base, "alice")
	bob := e2eRegisterLogin(t, base, "bob")
	wa := e2eDial(t, base, e2eTicket(t, base, alice.at))
	wb := e2eDial(t, base, e2eTicket(t, base, bob.at))
	waitHubCount(t, a, 2)

	// 2. public broadcast: alice CHAT S1 -> alice ACK + bob delivery.
	s1 := uuid.NewString()
	wa.send(t, &pb.WsMessage{Type: pb.MsgType_CHAT, Content: "hello hall", StanzaId: s1})
	ack1 := wa.await(t, "alice ACK S1", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_ACK && m.GetStanzaId() == s1
	})
	if ack1.GetSeq() <= 0 || ack1.GetTimestamp() <= 0 {
		t.Fatalf("ACK S1 insane: seq=%d ts=%d", ack1.GetSeq(), ack1.GetTimestamp())
	}
	got1 := wb.await(t, "bob CHAT S1", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_CHAT && m.GetStanzaId() == s1
	})
	if got1.GetFromUid() != alice.userID || got1.GetSeq() != ack1.GetSeq() || got1.GetContent() != "hello hall" {
		t.Fatalf("bob delivery wrong: %v (want from=%s seq=%d)", got1, alice.userID, ack1.GetSeq())
	}

	// 3. seed friendship in DB; alice->bob PRIVATE_CHAT S2.
	seedFriends(t, a, alice.userID, bob.userID)
	s2 := uuid.NewString()
	priv := &pb.WsMessage{Type: pb.MsgType_PRIVATE_CHAT, ToUid: bob.userID, Content: "hi bob", StanzaId: s2}
	wa.send(t, priv)
	ack2 := wa.await(t, "alice ACK S2", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_ACK && m.GetStanzaId() == s2
	})
	if ack2.GetSeq() <= 0 {
		t.Fatalf("ACK S2 bad seq %d", ack2.GetSeq())
	}
	got2 := wb.await(t, "bob PRIVATE S2", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_PRIVATE_CHAT && m.GetStanzaId() == s2
	})
	if got2.GetFromUid() != alice.userID || got2.GetSeq() != ack2.GetSeq() || got2.GetContent() != "hi bob" {
		t.Fatalf("bob private wrong: %v", got2)
	}

	// 4. no-friendship denial: carol->alice PRIVATE_CHAT -> 30001, alice gets NOTHING.
	carol := e2eRegisterLogin(t, base, "carol")
	wc := e2eDial(t, base, e2eTicket(t, base, carol.at))
	waitHubCount(t, a, 3)
	wc.send(t, &pb.WsMessage{Type: pb.MsgType_PRIVATE_CHAT, ToUid: alice.userID, Content: "spam", StanzaId: uuid.NewString()})
	deny := wc.await(t, "carol 30001 notice", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_SYSTEM_NOTICE
	})
	if c := noticeCode(t, deny); c != 30001 {
		t.Fatalf("deny notice code = %d, want 30001 (extra=%q)", c, deny.GetExtra())
	}
	if rest := wa.drain(negWindow); len(rest) != 0 {
		t.Fatalf("alice should receive NOTHING on denied private, got %d frames (%v)", len(rest), rest[0])
	}

	// 5. dedup window: resend IDENTICAL S2 -> same-seq ACK replay, bob gets no copy.
	wa.send(t, priv)
	ackReplay := wa.await(t, "alice ACK S2 replay", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_ACK && m.GetStanzaId() == s2
	})
	if ackReplay.GetSeq() != ack2.GetSeq() {
		t.Fatalf("replay seq = %d, want original %d", ackReplay.GetSeq(), ack2.GetSeq())
	}
	if rest := wb.drain(negWindow); len(rest) != 0 {
		t.Fatalf("bob should get no duplicate S2, got %d frames", len(rest))
	}

	// 6. restart recovery: shut down, rebuild on the SAME db file, reconnect,
	//    resend S2 -> ACK must carry the ORIGINAL S2 seq, bob gets no duplicate.
	_ = wa.conn.Close()
	_ = wb.conn.Close()
	_ = wc.conn.Close()
	srv.Close()
	if err := a.Close(); err != nil {
		t.Fatalf("Close app1: %v", err)
	}

	// Same deployment restarting: same config (same jwt secret), same DB file.
	a2, err := app.Build(cfg)
	if err != nil {
		t.Fatalf("rebuild on same DB: %v", err)
	}
	t.Cleanup(func() {
		if err := a2.Close(); err != nil {
			t.Errorf("Close app2: %v", err)
		}
	})
	srv2 := httptest.NewServer(a2.Engine)
	t.Cleanup(srv2.Close)
	base2 := srv2.URL

	// Old ATs still verify (same jwt secret): mint fresh tickets on app2.
	wa2 := e2eDial(t, base2, e2eTicket(t, base2, alice.at))
	wb2 := e2eDial(t, base2, e2eTicket(t, base2, bob.at))
	waitHubCount(t, a2, 2)

	wa2.send(t, priv)
	ackAfter := wa2.await(t, "alice ACK S2 post-restart", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_ACK && m.GetStanzaId() == s2
	})
	if ackAfter.GetSeq() != ack2.GetSeq() {
		t.Fatalf("post-restart replay seq = %d, want ORIGINAL %d", ackAfter.GetSeq(), ack2.GetSeq())
	}
	if rest := wb2.drain(negWindow); len(rest) != 0 {
		t.Fatalf("bob should get no duplicate S2 post-restart, got %d frames", len(rest))
	}

	// 7. heartbeat: app-level PING -> PONG. (The 30s server WS Ping is NOT
	//    awaited here — gorilla's default pong handler covers that path.)
	wa2.send(t, &pb.WsMessage{Type: pb.MsgType_HEARTBEAT_PING})
	wa2.await(t, "PONG", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_HEARTBEAT_PONG
	})
}
