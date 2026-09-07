// Stage-4 DoD proof (MISSION M3): group fan-out + echo-to-sender, stanza
// dedup replay, history authz, mute enforcement, signaling bypass with zero
// DB pollution, and kick revocation — all against the fully-wired app
// (app.Build + httptest server) with a self-contained harness.
package test

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
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
	s4Pass    = "password123"
	s4Timeout = 5 * time.Second
	// s4DedupSilence bounds the dedup "receives nothing" assertion.
	s4DedupSilence = 1500 * time.Millisecond
	// s4NegWindow bounds the remaining "receives nothing" assertions.
	s4NegWindow = time.Second
)

func s4Config(t *testing.T, dbPath string) *config.Config {
	t.Helper()
	var rb [16]byte
	if _, err := rand.Read(rb[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "127.0.0.1", Port: 8080,
			ReadTimeout: "30s", WriteTimeout: "30s", Mode: "test",
		},
		Security: config.SecurityConfig{
			JWTSecret:        "s4-" + hex.EncodeToString(rb[:]),
			JWTAccessExpire:  "2h",
			JWTRefreshExpire: "336h",
			BcryptCost:       bcrypt.MinCost, // speed; prod uses 10
		},
		Database: config.DatabaseConfig{
			Path: dbPath, MaxOpenConns: 1, MaxIdleConns: 1, BusyTimeout: "5000",
		},
		Admin: config.AdminConfig{Username: "superadmin", Password: "SuperAdmin123!"},
	}
	cfg.Media.StorageDir = filepath.Join(t.TempDir(), "media")
	return cfg
}

// ---------- HTTP helpers ----------

type s4Env struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

var s4HTTP = &http.Client{Timeout: 10 * time.Second}

// s4Do issues one JSON request and returns the raw HTTP status + envelope
// without failing on business-error codes (for negative assertions).
func s4Do(t *testing.T, method, base, path string, body interface{}, token string) (int, s4Env) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, base+path, rdr)
	if err != nil {
		t.Fatalf("build req: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s4HTTP.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var env s4Env
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("%s %s: bad envelope %q: %v", method, path, s4short(raw), err)
	}
	return resp.StatusCode, env
}

func s4short(b []byte) string {
	if len(b) > 160 {
		return string(b[:160]) + "..."
	}
	return string(b)
}

func s4mustOK(t *testing.T, method, path string, code int, env s4Env) json.RawMessage {
	t.Helper()
	if code == 200 || code == 201 {
		if env.Code != 0 {
			t.Fatalf("%s %s: code=%d msg=%q http=%d", method, path, env.Code, env.Msg, code)
		}
		return env.Data
	}
	t.Fatalf("%s %s: http=%d code=%d msg=%q, want 2xx/code 0", method, path, code, env.Code, env.Msg)
	return nil
}

func s4POST(t *testing.T, base, path string, body interface{}, token string) json.RawMessage {
	t.Helper()
	code, env := s4Do(t, http.MethodPost, base, path, body, token)
	return s4mustOK(t, http.MethodPost, path, code, env)
}

func s4GET(t *testing.T, base, path, token string) json.RawMessage {
	t.Helper()
	code, env := s4Do(t, http.MethodGet, base, path, nil, token)
	return s4mustOK(t, http.MethodGet, path, code, env)
}

func s4PATCH(t *testing.T, base, path string, body interface{}, token string) json.RawMessage {
	t.Helper()
	code, env := s4Do(t, http.MethodPatch, base, path, body, token)
	return s4mustOK(t, http.MethodPatch, path, code, env)
}

func s4DELETE(t *testing.T, base, path, token string) json.RawMessage {
	t.Helper()
	code, env := s4Do(t, http.MethodDelete, base, path, nil, token)
	return s4mustOK(t, http.MethodDelete, path, code, env)
}

type s4Ident struct {
	userID, at string
}

func s4RegisterLogin(t *testing.T, base, user string) s4Ident {
	t.Helper()
	data := s4POST(t, base, "/api/v1/auth/register",
		map[string]string{"username": user, "password": s4Pass}, "")
	var reg struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(data, &reg); err != nil || reg.UserID == "" {
		t.Fatalf("register %s: bad data %s", user, s4short(data))
	}
	data = s4POST(t, base, "/api/v1/auth/login", map[string]string{
		"username": user, "password": s4Pass,
		"device_class": "interactive", "device_name": "s4",
	}, "")
	var login struct {
		AccessToken string `json:"access_token"`
		UserID      string `json:"user_id"`
	}
	if err := json.Unmarshal(data, &login); err != nil || login.AccessToken == "" {
		t.Fatalf("login %s: bad data %s", user, s4short(data))
	}
	if login.UserID != reg.UserID {
		t.Fatalf("login uid %q != registered %q", login.UserID, reg.UserID)
	}
	return s4Ident{userID: reg.UserID, at: login.AccessToken}
}

func s4Ticket(t *testing.T, base, at string) string {
	t.Helper()
	data := s4POST(t, base, "/api/v1/auth/ticket", map[string]string{}, at)
	var out struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.Ticket == "" {
		t.Fatalf("ticket: bad data %s", s4short(data))
	}
	return out.Ticket
}

// ---------- WS helpers ----------

type s4ws struct {
	conn *websocket.Conn
	ch   chan *pb.WsMessage

	mu  sync.Mutex
	buf []*pb.WsMessage // pushed-back frames (survive negative windows)
}

func s4Dial(t *testing.T, base, ticket string) *s4ws {
	t.Helper()
	u := "ws" + strings.TrimPrefix(base, "http") + "/ws?ticket=" + url.QueryEscape(ticket)
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: s4Timeout}).Dial(u, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	w := &s4ws{conn: conn, ch: make(chan *pb.WsMessage, 256)}
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

func (w *s4ws) send(t *testing.T, m *pb.WsMessage) {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := w.conn.WriteMessage(websocket.BinaryMessage, b); err != nil {
		t.Fatalf("ws write: %v", err)
	}
}

// next returns the oldest pushed-back frame first, else the next live frame.
func (w *s4ws) next(timeout time.Duration) (*pb.WsMessage, error) {
	w.mu.Lock()
	if len(w.buf) > 0 {
		m := w.buf[0]
		w.buf = w.buf[1:]
		w.mu.Unlock()
		return m, nil
	}
	w.mu.Unlock()
	select {
	case m, ok := <-w.ch:
		if !ok {
			return nil, io.EOF
		}
		return m, nil
	case <-time.After(timeout):
		return nil, io.ErrNoProgress
	}
}

func (w *s4ws) unread(m *pb.WsMessage) {
	w.mu.Lock()
	w.buf = append([]*pb.WsMessage{m}, w.buf...)
	w.mu.Unlock()
}

// await consumes frames until pred matches; non-matching frames are pushed
// back in order so later assertions still see them.
func (w *s4ws) await(t *testing.T, what string, pred func(*pb.WsMessage) bool) *pb.WsMessage {
	t.Helper()
	deadline := time.Now().Add(s4Timeout)
	var skipped []*pb.WsMessage
	for {
		rest := time.Until(deadline)
		if rest <= 0 {
			t.Fatalf("timeout waiting for %s", what)
		}
		m, err := w.next(rest)
		if err != nil {
			t.Fatalf("waiting for %s: %v", what, err)
		}
		if pred(m) {
			w.mu.Lock()
			w.buf = append(skipped, w.buf...)
			w.mu.Unlock()
			return m
		}
		skipped = append(skipped, m)
	}
}

// assertSilent fails if any frame matching pred arrives within window.
// Non-matching frames are pushed back untouched.
func (w *s4ws) assertSilent(t *testing.T, what string, pred func(*pb.WsMessage) bool, window time.Duration) {
	t.Helper()
	deadline := time.Now().Add(window)
	var skipped []*pb.WsMessage
	for {
		rest := time.Until(deadline)
		if rest <= 0 {
			break
		}
		m, err := w.next(rest)
		if err != nil {
			break // timeout or closed: window over
		}
		if pred(m) {
			t.Fatalf("%s: unexpected frame type=%s from=%s stanza=%s content=%q",
				what, m.GetType(), m.GetFromUid(), m.GetStanzaId(), m.GetContent())
		}
		skipped = append(skipped, m)
	}
	w.mu.Lock()
	w.buf = append(skipped, w.buf...)
	w.mu.Unlock()
}

func s4NoticeCode(t *testing.T, m *pb.WsMessage) int {
	t.Helper()
	var e struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal([]byte(m.GetExtra()), &e); err != nil {
		t.Fatalf("notice Extra not coded JSON %q: %v", m.GetExtra(), err)
	}
	return e.Code
}

func s4WaitHub(t *testing.T, a *app.App, want int) {
	t.Helper()
	deadline := time.Now().Add(s4Timeout)
	for time.Now().Before(deadline) {
		if a.Hub.Count() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("hub Count = %d, want %d", a.Hub.Count(), want)
}

func s4Counts(t *testing.T, a *app.App) (msgs, convs int64) {
	t.Helper()
	if err := a.DB.Model(&store.Message{}).Count(&msgs).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if err := a.DB.Model(&store.Conversation{}).Count(&convs).Error; err != nil {
		t.Fatalf("count conversations: %v", err)
	}
	return msgs, convs
}

// ---------- group JSON shapes ----------

type s4Member struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type s4HistoryMsg struct {
	Seq      int64  `json:"seq"`
	FromUID  string `json:"from_uid"`
	Content  string `json:"content"`
	StanzaID string `json:"stanza_id"`
}

// ---------- the Stage-4 DoD test ----------

func TestStage4EndToEnd(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "e2e-s4.db")
	a, err := app.Build(s4Config(t, dbPath))
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

	// 1. Four users; alice/bob/carol on WS (interactive class).
	alice := s4RegisterLogin(t, base, "alice")
	bob := s4RegisterLogin(t, base, "bob")
	carol := s4RegisterLogin(t, base, "carol")
	dave := s4RegisterLogin(t, base, "dave")
	wa := s4Dial(t, base, s4Ticket(t, base, alice.at))
	wb := s4Dial(t, base, s4Ticket(t, base, bob.at))
	wc := s4Dial(t, base, s4Ticket(t, base, carol.at))
	s4WaitHub(t, a, 3)

	// 2. REST: alice creates, invites bob, carol joins; alice is owner.
	data := s4POST(t, base, "/api/v1/groups", map[string]string{"name": "stage4"}, alice.at)
	var created struct {
		GroupID string `json:"group_id"`
	}
	if err := json.Unmarshal(data, &created); err != nil || created.GroupID == "" {
		t.Fatalf("create group: bad data %s", s4short(data))
	}
	gid := created.GroupID
	s4POST(t, base, "/api/v1/groups/"+gid+"/invite",
		map[string]interface{}{"user_ids": []string{bob.userID}}, alice.at)
	s4POST(t, base, "/api/v1/groups/"+gid+"/join", map[string]string{}, carol.at)

	data = s4GET(t, base, "/api/v1/groups/"+gid+"/members", alice.at)
	var members []s4Member
	if err := json.Unmarshal(data, &members); err != nil {
		t.Fatalf("members: bad data %s", s4short(data))
	}
	ownerOK := false
	for _, m := range members {
		if m.UserID == alice.userID && m.Role == "owner" {
			ownerOK = true
		}
	}
	if !ownerOK {
		t.Fatalf("alice not owner in members %s", s4short(data))
	}

	// 3. Fan-out + echo: alice GROUP_CHAT S1.
	c1 := "s4-c1-hello"
	s1 := uuid.NewString()
	frame1 := &pb.WsMessage{Type: pb.MsgType_GROUP_CHAT, ToUid: gid, Content: c1, StanzaId: s1}
	wa.send(t, frame1)
	ack1 := wa.await(t, "alice ACK S1", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_ACK && m.GetStanzaId() == s1
	})
	if ack1.GetSeq() <= 0 {
		t.Fatalf("ACK S1 bad seq %d", ack1.GetSeq())
	}
	echo := wa.await(t, "alice echo S1", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_GROUP_CHAT && m.GetStanzaId() == s1
	})
	if echo.GetSeq() != ack1.GetSeq() || echo.GetFromUid() != alice.userID ||
		echo.GetToUid() != gid || echo.GetContent() != c1 {
		t.Fatalf("echo wrong: %v (want seq=%d from=%s to=%s content=%q)",
			echo, ack1.GetSeq(), alice.userID, gid, c1)
	}
	for name, w := range map[string]*s4ws{"bob": wb, "carol": wc} {
		got := w.await(t, name+" GROUP_CHAT S1", func(m *pb.WsMessage) bool {
			return m.GetType() == pb.MsgType_GROUP_CHAT && m.GetStanzaId() == s1
		})
		if got.GetSeq() != ack1.GetSeq() || got.GetFromUid() != alice.userID ||
			got.GetToUid() != gid || got.GetContent() != c1 {
			t.Fatalf("%s delivery wrong: %v", name, got)
		}
	}

	// 4. Dedup: resend the SAME frame; same-seq ACK, no re-fan-out.
	wa.send(t, frame1)
	ackReplay := wa.await(t, "alice ACK S1 replay", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_ACK && m.GetStanzaId() == s1
	})
	if ackReplay.GetSeq() != ack1.GetSeq() {
		t.Fatalf("replay seq = %d, want original %d", ackReplay.GetSeq(), ack1.GetSeq())
	}
	isS1 := func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_GROUP_CHAT && m.GetStanzaId() == s1
	}
	wb.assertSilent(t, "bob duplicate S1", isS1, s4DedupSilence)
	wc.assertSilent(t, "carol duplicate S1", isS1, s4DedupSilence)

	// 5. History authz: bob sees C1; dave (outsider) gets 40002.
	data = s4GET(t, base, "/api/v1/groups/"+gid+"/history?limit=50", bob.at)
	var hist struct {
		Messages []s4HistoryMsg `json:"messages"`
		HasMore  bool           `json:"has_more"`
	}
	if err := json.Unmarshal(data, &hist); err != nil {
		t.Fatalf("history: bad data %s", s4short(data))
	}
	found := false
	for _, m := range hist.Messages {
		if m.StanzaID == s1 {
			found = true
			if m.Content != c1 || m.Seq != ack1.GetSeq() || m.FromUID != alice.userID {
				t.Fatalf("history C1 wrong: %+v (want seq=%d content=%q)", m, ack1.GetSeq(), c1)
			}
		}
	}
	if !found {
		t.Fatalf("history missing C1 stanza %s (%s)", s1, s4short(data))
	}
	if code, env := s4Do(t, http.MethodGet, base, "/api/v1/groups/"+gid+"/history?limit=50", nil, dave.at); code == 200 || env.Code != 40002 {
		t.Fatalf("dave history: http=%d code=%d msg=%q, want 40002 non-200", code, env.Code, env.Msg)
	}

	// 6. Mute: alice mutes carol; carol S2 denied 40005, bob silent; unmute; resend OK.
	s4PATCH(t, base, "/api/v1/groups/"+gid+"/members/"+carol.userID+"/mute",
		map[string]bool{"muted": true}, alice.at)
	s2 := uuid.NewString()
	c2 := "s4-c2-muted-probe"
	wc.send(t, &pb.WsMessage{Type: pb.MsgType_GROUP_CHAT, ToUid: gid, Content: c2, StanzaId: s2})
	muteNotice := wc.await(t, "carol 40005 notice", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_SYSTEM_NOTICE
	})
	if c := s4NoticeCode(t, muteNotice); c != 40005 {
		t.Fatalf("mute notice code = %d, want 40005 (extra=%q)", c, muteNotice.GetExtra())
	}
	wb.assertSilent(t, "bob during carol mute", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_GROUP_CHAT && m.GetStanzaId() == s2
	}, s4NegWindow)

	s4PATCH(t, base, "/api/v1/groups/"+gid+"/members/"+carol.userID+"/mute",
		map[string]bool{"muted": false}, alice.at)
	wc.send(t, &pb.WsMessage{Type: pb.MsgType_GROUP_CHAT, ToUid: gid, Content: c2, StanzaId: s2})
	ack2 := wc.await(t, "carol ACK S2 after unmute", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_ACK && m.GetStanzaId() == s2
	})
	if ack2.GetSeq() <= 0 {
		t.Fatalf("ACK S2 bad seq %d", ack2.GetSeq())
	}
	got2 := wb.await(t, "bob GROUP_CHAT S2", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_GROUP_CHAT && m.GetStanzaId() == s2
	})
	if got2.GetSeq() != ack2.GetSeq() || got2.GetFromUid() != carol.userID || got2.GetContent() != c2 {
		t.Fatalf("bob S2 delivery wrong: %v", got2)
	}
	echo2 := wa.await(t, "alice GROUP_CHAT S2 echo", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_GROUP_CHAT && m.GetStanzaId() == s2
	})
	if echo2.GetSeq() != ack2.GetSeq() || echo2.GetFromUid() != carol.userID || echo2.GetContent() != c2 {
		t.Fatalf("alice S2 echo wrong: %v", echo2)
	}

	// 7. Signaling bypass: alice<->bob friend up, counts frozen, offer bypasses DB.
	s4POST(t, base, "/api/v1/friends/apply", map[string]string{"target_username": "bob"}, alice.at)
	wb.await(t, "bob FRIEND_APPLY_NOTIFY", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_FRIEND_APPLY_NOTIFY && m.GetFromUid() == alice.userID
	})
	s4POST(t, base, "/api/v1/friends/respond",
		map[string]string{"target_user_id": alice.userID, "action": "accept"}, bob.at)
	wa.await(t, "alice FRIEND_ACCEPT_NOTIFY", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_FRIEND_ACCEPT_NOTIFY && m.GetFromUid() == bob.userID
	})
	msgBefore, convBefore := s4Counts(t, a)
	s3 := uuid.NewString()
	offerPayload := []byte("sdp-offer-x")
	wa.send(t, &pb.WsMessage{
		Type: pb.MsgType_SIGNALING_OFFER, ToUid: bob.userID, Payload: offerPayload, StanzaId: s3,
	})
	sig := wb.await(t, "bob SIGNALING_OFFER S3", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_SIGNALING_OFFER && m.GetStanzaId() == s3
	})
	if sig.GetFromUid() != alice.userID || sig.GetSeq() != 0 ||
		!bytes.Equal(sig.GetPayload(), offerPayload) {
		t.Fatalf("signaling delivery wrong: from=%q seq=%d payload=%q (want from=%s seq=0 payload=%q)",
			sig.GetFromUid(), sig.GetSeq(), sig.GetPayload(), alice.userID, offerPayload)
	}
	time.Sleep(300 * time.Millisecond) // let any stray persist land
	if msgAfter, convAfter := s4Counts(t, a); msgAfter != msgBefore || convAfter != convBefore {
		t.Fatalf("signaling polluted DB: messages %d->%d conversations %d->%d",
			msgBefore, msgAfter, convBefore, convAfter)
	}

	// Non-friend signaling denied: dave -> alice yields 30001, alice hears nothing.
	wd := s4Dial(t, base, s4Ticket(t, base, dave.at))
	s4WaitHub(t, a, 4)
	wd.send(t, &pb.WsMessage{
		Type: pb.MsgType_SIGNALING_OFFER, ToUid: alice.userID,
		Payload: []byte("spam"), StanzaId: uuid.NewString(),
	})
	deny := wd.await(t, "dave 30001 notice", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_SYSTEM_NOTICE
	})
	if c := s4NoticeCode(t, deny); c != 30001 {
		t.Fatalf("signaling deny code = %d, want 30001 (extra=%q)", c, deny.GetExtra())
	}
	wa.assertSilent(t, "alice on denied signaling", func(m *pb.WsMessage) bool { return true }, s4NegWindow)

	// 8. Kick revocation: alice kicks carol; after invalidation carol is gated.
	s4DELETE(t, base, "/api/v1/groups/"+gid+"/members/"+carol.userID, alice.at)
	// Production caveat: without invalidation the membership cache may allow
	// sends for up to 60s — the test uses the exported invalidation hook.
	a.Router.InvalidateGroupMember(gid, carol.userID)
	s4 := uuid.NewString()
	wc.send(t, &pb.WsMessage{Type: pb.MsgType_GROUP_CHAT, ToUid: gid, Content: "s4-c4-kicked", StanzaId: s4})
	kickNotice := wc.await(t, "carol 40002 notice", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_SYSTEM_NOTICE
	})
	if c := s4NoticeCode(t, kickNotice); c != 40002 {
		t.Fatalf("kick notice code = %d, want 40002 (extra=%q)", c, kickNotice.GetExtra())
	}
	isS4 := func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_GROUP_CHAT && m.GetStanzaId() == s4
	}
	wb.assertSilent(t, "bob after carol kick", isS4, s4NegWindow)
	wa.assertSilent(t, "alice after carol kick", isS4, s4NegWindow)
}
