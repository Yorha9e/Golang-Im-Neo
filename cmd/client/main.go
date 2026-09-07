// Command imcli is the Stage-1+2+4 command-line verification client
// (MISSIONS M5 + M4, Stage-4 M3).
//
// Protobuf binary frames both ways (golang-im-neo-system/proto). Actions:
//
//	register   POST /api/v1/auth/register, print user_id
//	login      POST login, print AT/RT/session_id (cache to .imcli-<user>.json)
//	ticket     POST /api/v1/auth/ticket with cached AT, print ticket
//	chat       login -> ticket -> WS dial -> scripted send (-msg [-to]) with
//	           ACK wait + -listen keep-reading window
//	dedup-test fixed-stanza double-send, assert identical ACK seq (REPLAY PASS)
//	e2e        two-user public-broadcast demo with PASS/FAIL summary
//	friend-apply/-respond/-list/-pending/-delete   friend endpoints (cached login)
//	media-upload multipart file upload, print mid + access_url
//	admin-ban/-kick/-broadcast  admin endpoints (needs admin login)
//	group-create/-list/-info/-members/-invite/-join/-leave/-dismiss/-kick/
//	-role/-mute/-unmute/-history/-chat   Stage-4 group endpoints + WS fan-out
//	signal-send/-listen   Stage-4 WebRTC signaling bypass frames over WS
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	pb "golang-im-neo-system/proto"
)

var (
	fServer  = flag.String("server", "http://127.0.0.1:8080", "server HTTP base URL")
	fUser    = flag.String("user", "", "username")
	fPass    = flag.String("pass", "", "password")
	fDevice  = flag.String("device", "interactive", "device class (interactive|hardware)")
	fDevName = flag.String("devname", "", "device name (defaults to <device>-cli)")
	fAction  = flag.String("action", "", "action: register|login|ticket|chat|dedup-test|e2e|friend-apply|friend-respond|friend-list|friend-pending|friend-delete|media-upload|admin-ban|admin-kick|admin-broadcast|group-create|group-list|group-info|group-members|group-invite|group-join|group-leave|group-dismiss|group-kick|group-role|group-mute|group-unmute|group-history|group-chat|signal-send|signal-listen")
	fMsg     = flag.String("msg", "", "chat text (chat action) or broadcast content (admin-broadcast)")
	fTo      = flag.String("to", "", "recipient user_id for PRIVATE_CHAT (chat action)")
	fListen  = flag.Duration("listen", 5*time.Second, "keep-reading window after sends (chat action)")
	// Stage-2 flags.
	fTarget   = flag.String("target", "", "friend-apply: target username; friend-respond/friend-delete: target user_id; admin-ban: user_id to ban")
	fRemark   = flag.String("remark", "", "remark for friend-apply")
	fAction2  = flag.String("action2", "accept", "friend-respond decision: accept|reject")
	fFile     = flag.String("file", "", "local file path for media-upload")
	fMediaTyp = flag.String("mediatype", "avatar", "media type for media-upload (avatar|image|voice|video)")
	fAccess   = flag.String("access", "public", "access level for media-upload (public|private)")
	fSession  = flag.String("session", "", "session_id for admin-kick")
	// Stage-4 flags.
	fGroup = flag.String("group", "", "group id for group-* actions")
	fLimit = flag.Int("limit", 50, "history page size for group-history")
)

const ackTimeout = 5 * time.Second

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "imcli: "+format+"\n", args...)
	os.Exit(1)
}

// ---------- HTTP envelope ----------

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

func postJSON(base, path string, body interface{}, token string) (json.RawMessage, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = strings.NewReader("{}")
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimSuffix(base, "/")+path, rdr)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w (is the server up?)", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("bad envelope (http=%d body=%q): %w", resp.StatusCode, short(raw), err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("server code=%d msg=%q (http=%d)", env.Code, env.Msg, resp.StatusCode)
	}
	return env.Data, nil
}

func short(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "..."
	}
	return string(b)
}

// doJSON issues one JSON request with method and decodes the envelope.
func doJSON(method, base, path string, body interface{}, token string) (json.RawMessage, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		rdr = bytes.NewReader(b)
	} else if method == http.MethodPost {
		rdr = strings.NewReader("{}")
	}
	req, err := http.NewRequest(method, strings.TrimSuffix(base, "/")+path, rdr)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if body != nil || method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w (is the server up?)", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("bad envelope (http=%d body=%q): %w", resp.StatusCode, short(raw), err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("server code=%d msg=%q (http=%d)", env.Code, env.Msg, resp.StatusCode)
	}
	return env.Data, nil
}

// getJSON issues an authenticated GET and decodes the envelope data.
func getJSON(base, path, token string) (json.RawMessage, error) {
	return doJSON(http.MethodGet, base, path, nil, token)
}

// deleteJSON issues an authenticated DELETE and decodes the envelope data.
func deleteJSON(base, path, token string) (json.RawMessage, error) {
	return doJSON(http.MethodDelete, base, path, nil, token)
}

// postMultipart uploads one file plus form fields and decodes the envelope.
func postMultipart(base, path, token, filePath string, fields map[string]string) (json.RawMessage, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(fw, f); err != nil {
		return nil, fmt.Errorf("write file part: %w", err)
	}
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return nil, fmt.Errorf("write field %s: %w", k, err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close multipart: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimSuffix(base, "/")+path, &buf)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w (is the server up?)", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("bad envelope (http=%d body=%q): %w", resp.StatusCode, short(raw), err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("server code=%d msg=%q (http=%d)", env.Code, env.Msg, resp.StatusCode)
	}
	return env.Data, nil
}

// ---------- session cache ----------

type sessionCache struct {
	Server       string `json:"server"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id"`
	SessionID    string `json:"session_id"`
}

func cachePath(user string) string { return ".imcli-" + user + ".json" }

func saveCache(user string, s *sessionCache) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cachePath(user), b, 0600)
}

func loadCache(user string) (*sessionCache, error) {
	b, err := os.ReadFile(cachePath(user))
	if err != nil {
		return nil, err
	}
	var s sessionCache
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ---------- auth flows ----------

func doRegister(base, user, pass string) string {
	data, err := postJSON(base, "/api/v1/auth/register", map[string]string{
		"username": user, "password": pass,
	}, "")
	if err != nil {
		fatalf("register: %v", err)
	}
	var out struct {
		UserID   string `json:"user_id"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.UserID == "" {
		fatalf("register: bad response %s", short(data))
	}
	fmt.Printf("registered username=%s user_id=%s\n", out.Username, out.UserID)
	return out.UserID
}

func doLogin(base, user, pass, device, devName string) *sessionCache {
	if devName == "" {
		devName = device + "-cli"
	}
	data, err := postJSON(base, "/api/v1/auth/login", map[string]string{
		"username": user, "password": pass,
		"device_class": device, "device_name": devName,
	}, "")
	if err != nil {
		fatalf("login: %v", err)
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		UserID       string `json:"user_id"`
		SessionID    string `json:"session_id"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.AccessToken == "" {
		fatalf("login: bad response %s", short(data))
	}
	s := &sessionCache{Server: base, AccessToken: out.AccessToken,
		RefreshToken: out.RefreshToken, UserID: out.UserID, SessionID: out.SessionID}
	if err := saveCache(user, s); err != nil {
		fmt.Fprintf(os.Stderr, "imcli: warning: cannot cache session: %v\n", err)
	}
	fmt.Printf("login ok user_id=%s session_id=%s\nAT=%s\nRT=%s\n",
		out.UserID, out.SessionID, out.AccessToken, out.RefreshToken)
	return s
}

// ensureLogin returns a cached session, logging in with -pass when needed.
func ensureLogin() *sessionCache {
	if *fUser == "" {
		fatalf("need -user")
	}
	if s, err := loadCache(*fUser); err == nil && s.AccessToken != "" {
		return s
	}
	if *fPass == "" {
		fatalf("no cached session for %q (run login first or pass -pass)", *fUser)
	}
	return doLogin(*fServer, *fUser, *fPass, *fDevice, *fDevName)
}

func fetchTicket(s *sessionCache) string {
	data, err := postJSON(s.Server, "/api/v1/auth/ticket", map[string]string{}, s.AccessToken)
	if err != nil {
		fatalf("ticket: %v (try login again — AT may be expired)", err)
	}
	var out struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.Ticket == "" {
		fatalf("ticket: bad response %s", short(data))
	}
	return out.Ticket
}

// ---------- WS ----------

type wsClient struct {
	conn   *websocket.Conn
	ch     chan *pb.WsMessage
	closed chan struct{}
}

func wsBase(httpBase string) string {
	b := strings.TrimSuffix(httpBase, "/")
	switch {
	case strings.HasPrefix(b, "https://"):
		return "wss://" + strings.TrimPrefix(b, "https://")
	case strings.HasPrefix(b, "http://"):
		return "ws://" + strings.TrimPrefix(b, "http://")
	default:
		return "ws://" + b
	}
}

func dialWS(httpBase, ticket string) *wsClient {
	u := wsBase(httpBase) + "/ws?ticket=" + url.QueryEscape(ticket)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(u, nil)
	if err != nil {
		fatalf("ws dial: %v (bad/expired ticket?)", err)
	}
	w := &wsClient{conn: conn, ch: make(chan *pb.WsMessage, 256), closed: make(chan struct{})}
	go w.readLoop()
	return w
}

func (w *wsClient) readLoop() {
	defer close(w.ch)
	for {
		typ, data, err := w.conn.ReadMessage()
		if err != nil {
			return
		}
		if typ != websocket.BinaryMessage {
			continue
		}
		m := &pb.WsMessage{}
		if err := proto.Unmarshal(data, m); err != nil {
			fmt.Fprintf(os.Stderr, "imcli: warn: undecodable frame (%d bytes): %v\n", len(data), err)
			continue
		}
		fmt.Printf("[ws] type=%s from=%s to=%s seq=%d stanza=%s content=%q\n",
			m.GetType(), m.GetFromUid(), m.GetToUid(), m.GetSeq(), m.GetStanzaId(), m.GetContent())
		select {
		case w.ch <- m:
		case <-w.closed:
			return
		}
	}
}

func (w *wsClient) send(m *pb.WsMessage) error {
	b, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	return w.conn.WriteMessage(websocket.BinaryMessage, b)
}

func (w *wsClient) next(timeout time.Duration) (*pb.WsMessage, error) {
	select {
	case m, ok := <-w.ch:
		if !ok {
			return nil, fmt.Errorf("connection closed by server")
		}
		return m, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for frame")
	}
}

func (w *wsClient) close() {
	close(w.closed)
	_ = w.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"))
	_ = w.conn.Close()
}

// awaitACK consumes frames until the ACK for stanza arrives or timeout hits.
func awaitACK(w *wsClient, stanza string, timeout time.Duration) (*pb.WsMessage, error) {
	deadline := time.Now().Add(timeout)
	for {
		rest := time.Until(deadline)
		if rest <= 0 {
			return nil, fmt.Errorf("timeout waiting for ACK stanza=%s", stanza)
		}
		m, err := w.next(rest)
		if err != nil {
			return nil, err
		}
		if m.GetType() == pb.MsgType_ACK && m.GetStanzaId() == stanza {
			return m, nil
		}
	}
}

// awaitFrame consumes frames until pred matches or timeout hits.
func awaitFrame(w *wsClient, pred func(*pb.WsMessage) bool, what string, timeout time.Duration) (*pb.WsMessage, error) {
	deadline := time.Now().Add(timeout)
	for {
		rest := time.Until(deadline)
		if rest <= 0 {
			return nil, fmt.Errorf("timeout waiting for %s", what)
		}
		m, err := w.next(rest)
		if err != nil {
			return nil, err
		}
		if pred(m) {
			return m, nil
		}
	}
}

// ---------- actions ----------

func actChat() {
	sess := ensureLogin()
	w := dialWS(sess.Server, fetchTicket(sess))
	defer w.close()

	sentStanza := ""
	if *fMsg != "" {
		sentStanza = uuid.NewString()
		out := &pb.WsMessage{Content: *fMsg, StanzaId: sentStanza}
		if *fTo != "" {
			out.Type = pb.MsgType_PRIVATE_CHAT
			out.ToUid = *fTo
			fmt.Printf("-> PRIVATE_CHAT to=%s stanza=%s\n", *fTo, sentStanza)
		} else {
			out.Type = pb.MsgType_CHAT
			fmt.Printf("-> CHAT stanza=%s\n", sentStanza)
		}
		if err := w.send(out); err != nil {
			fatalf("send: %v", err)
		}
		ack, err := awaitACK(w, sentStanza, ackTimeout)
		if err != nil {
			fatalf("no ACK: %v", err)
		}
		fmt.Printf("ACK seq=%d stanza=%s\n", ack.GetSeq(), ack.GetStanzaId())
	}

	if *fListen > 0 {
		fmt.Printf("listening for %s ...\n", *fListen)
		deadline := time.Now().Add(*fListen)
		for {
			rest := time.Until(deadline)
			if rest <= 0 {
				break
			}
			if _, err := w.next(rest); err != nil {
				break // timeout ends the window; closed conn aborts early
			}
		}
	}
	fmt.Println("done")
}

func actDedupTest() {
	sess := ensureLogin()
	w := dialWS(sess.Server, fetchTicket(sess))
	defer w.close()

	stanza := uuid.NewString() // FIXED for this run: sent twice verbatim
	frame := &pb.WsMessage{Type: pb.MsgType_CHAT, Content: "dedup-probe", StanzaId: stanza}
	if err := w.send(frame); err != nil {
		fatalf("send #1: %v", err)
	}
	ack1, err := awaitACK(w, stanza, ackTimeout)
	if err != nil {
		fatalf("ACK #1: %v", err)
	}
	fmt.Printf("ACK #1 seq=%d\n", ack1.GetSeq())

	if err := w.send(frame); err != nil {
		fatalf("send #2 (replay): %v", err)
	}
	ack2, err := awaitACK(w, stanza, ackTimeout)
	if err != nil {
		fatalf("ACK #2: %v", err)
	}
	fmt.Printf("ACK #2 seq=%d\n", ack2.GetSeq())

	if ack1.GetSeq() > 0 && ack1.GetSeq() == ack2.GetSeq() {
		fmt.Printf("REPLAY seq %d PASS\n", ack1.GetSeq())
		return
	}
	fatalf("REPLAY FAIL: seq %d vs %d", ack1.GetSeq(), ack2.GetSeq())
}

func randSuffix() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func actE2E() {
	base := *fServer
	suffix := randSuffix()
	alice, bob := "e2e_a_"+suffix, "e2e_b_"+suffix
	pass := "password123"

	uidA := doRegister(base, alice, pass)
	uidB := doRegister(base, bob, pass)
	sessA := doLogin(base, alice, pass, "interactive", "")
	sessB := doLogin(base, bob, pass, "interactive", "")
	wa := dialWS(sessA.Server, fetchTicket(sessA))
	defer wa.close()
	wb := dialWS(sessB.Server, fetchTicket(sessB))
	defer wb.close()

	stanza := uuid.NewString()
	content := "hello from " + alice
	if err := wa.send(&pb.WsMessage{Type: pb.MsgType_CHAT, Content: content, StanzaId: stanza}); err != nil {
		fatalf("e2e send: %v", err)
	}
	ack, err := awaitACK(wa, stanza, ackTimeout)
	if err != nil || ack.GetSeq() <= 0 {
		fatalf("e2e FAIL: alice got no valid ACK: %v", err)
	}
	got, err := awaitFrame(wb, func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_CHAT && m.GetStanzaId() == stanza
	}, "bob delivery", ackTimeout)
	if err != nil {
		fatalf("e2e FAIL: bob got no delivery: %v", err)
	}
	ok := got.GetFromUid() == uidA && got.GetSeq() == ack.GetSeq() && got.GetContent() == content
	fmt.Printf("e2e: alice=%s bob=%s ack_seq=%d delivery_seq=%d\n", uidA, uidB, ack.GetSeq(), got.GetSeq())
	if !ok {
		fatalf("e2e FAIL: delivery mismatch (want from=%s seq=%d content=%q)", uidA, ack.GetSeq(), content)
	}
	fmt.Println("e2e PASS: public broadcast ACK+delivery verified")
}

// ---------- Stage-2 actions (friend / media / admin) ----------

func actFriendApply() {
	sess := ensureLogin()
	if *fTarget == "" {
		fatalf("friend-apply needs -target <username> [-remark <text>]")
	}
	data, err := postJSON(sess.Server, "/api/v1/friends/apply", map[string]string{
		"target_username": *fTarget, "remark": *fRemark,
	}, sess.AccessToken)
	if err != nil {
		fatalf("friend-apply: %v", err)
	}
	var out struct {
		TargetUserID string `json:"target_user_id"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		fatalf("friend-apply: bad response %s", short(data))
	}
	fmt.Printf("apply ok target_user_id=%s\n", out.TargetUserID)
}

func actFriendRespond() {
	sess := ensureLogin()
	if *fTarget == "" {
		fatalf("friend-respond needs -target <applicant_user_id> [-action2 accept|reject]")
	}
	if *fAction2 != "accept" && *fAction2 != "reject" {
		fatalf("friend-respond: -action2 must be accept or reject")
	}
	data, err := postJSON(sess.Server, "/api/v1/friends/respond", map[string]string{
		"target_user_id": *fTarget, "action": *fAction2,
	}, sess.AccessToken)
	if err != nil {
		fatalf("friend-respond: %v", err)
	}
	fmt.Printf("respond ok %s\n", short(data))
}

func actFriendList() {
	sess := ensureLogin()
	data, err := getJSON(sess.Server, "/api/v1/friends", sess.AccessToken)
	if err != nil {
		fatalf("friend-list: %v", err)
	}
	fmt.Printf("friends: %s\n", short(data))
}

func actFriendPending() {
	sess := ensureLogin()
	data, err := getJSON(sess.Server, "/api/v1/friends/pending", sess.AccessToken)
	if err != nil {
		fatalf("friend-pending: %v", err)
	}
	fmt.Printf("pending: %s\n", short(data))
}

func actFriendDelete() {
	sess := ensureLogin()
	if *fTarget == "" {
		fatalf("friend-delete needs -target <friend_user_id>")
	}
	data, err := deleteJSON(sess.Server, "/api/v1/friends/"+*fTarget, sess.AccessToken)
	if err != nil {
		fatalf("friend-delete: %v", err)
	}
	fmt.Printf("delete ok %s\n", short(data))
}

func actMediaUpload() {
	sess := ensureLogin()
	if *fFile == "" {
		fatalf("media-upload needs -file <path> [-mediatype avatar|image|voice|video] [-access public|private]")
	}
	data, err := postMultipart(sess.Server, "/api/v1/media/upload", sess.AccessToken, *fFile,
		map[string]string{"media_type": *fMediaTyp, "access_level": *fAccess})
	if err != nil {
		fatalf("media-upload: %v", err)
	}
	var out struct {
		MID       string `json:"mid"`
		AccessURL string `json:"access_url"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.MID == "" {
		fatalf("media-upload: bad response %s", short(data))
	}
	fmt.Printf("upload ok mid=%s access_url=%s\n", out.MID, out.AccessURL)
}

func actAdminBan() {
	sess := ensureLogin() // must be an admin login, e.g. -user superadmin
	if *fTarget == "" {
		fatalf("admin-ban needs -target <user_id>")
	}
	if _, err := postJSON(sess.Server, "/api/v1/admin/users/"+*fTarget+"/ban", map[string]string{}, sess.AccessToken); err != nil {
		fatalf("admin-ban: %v", err)
	}
	fmt.Printf("ban ok user_id=%s\n", *fTarget)
}

func actAdminKick() {
	sess := ensureLogin() // must be an admin login, e.g. -user superadmin
	if *fSession == "" {
		fatalf("admin-kick needs -session <session_id>")
	}
	if _, err := postJSON(sess.Server, "/api/v1/admin/sessions/"+*fSession+"/kick", map[string]string{}, sess.AccessToken); err != nil {
		fatalf("admin-kick: %v", err)
	}
	fmt.Printf("kick ok session_id=%s\n", *fSession)
}

func actAdminBroadcast() {
	sess := ensureLogin() // must be an admin login, e.g. -user superadmin
	if *fMsg == "" {
		fatalf("admin-broadcast needs -msg <content>")
	}
	if _, err := postJSON(sess.Server, "/api/v1/admin/broadcast", map[string]string{"content": *fMsg}, sess.AccessToken); err != nil {
		fatalf("admin-broadcast: %v", err)
	}
	fmt.Printf("broadcast ok content=%q\n", *fMsg)
}

// ---------- Stage-4 actions (group + signaling) ----------

func groupPath(id, suffix string) string {
	return "/api/v1/groups/" + url.PathEscape(id) + suffix
}

func needGroup() string {
	if *fGroup == "" {
		fatalf("this action needs -group <id>")
	}
	return *fGroup
}

func actGroupCreate() {
	sess := ensureLogin()
	if *fMsg == "" {
		fatalf("group-create needs -msg <name>")
	}
	data, err := postJSON(sess.Server, "/api/v1/groups", map[string]string{"name": *fMsg}, sess.AccessToken)
	if err != nil {
		fatalf("group-create: %v", err)
	}
	var out struct {
		GroupID string `json:"group_id"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.GroupID == "" {
		fatalf("group-create: bad response %s", short(data))
	}
	fmt.Printf("group created group_id=%s name=%q\n", out.GroupID, out.Name)
}

func actGroupList() {
	sess := ensureLogin()
	data, err := getJSON(sess.Server, "/api/v1/groups", sess.AccessToken)
	if err != nil {
		fatalf("group-list: %v", err)
	}
	var groups []struct {
		GroupID     string `json:"group_id"`
		Name        string `json:"name"`
		OwnerID     string `json:"owner_id"`
		MemberCount int64  `json:"member_count"`
	}
	if err := json.Unmarshal(data, &groups); err != nil {
		fatalf("group-list: bad response %s", short(data))
	}
	fmt.Printf("%-36s %-24s %-36s members\n", "GROUP_ID", "NAME", "OWNER")
	for _, g := range groups {
		fmt.Printf("%-36s %-24s %-36s %d\n", g.GroupID, g.Name, g.OwnerID, g.MemberCount)
	}
}

func actGroupInfo() {
	sess := ensureLogin()
	id := needGroup()
	data, err := getJSON(sess.Server, groupPath(id, ""), sess.AccessToken)
	if err != nil {
		fatalf("group-info: %v", err)
	}
	var out struct {
		GroupID     string `json:"group_id"`
		Name        string `json:"name"`
		OwnerID     string `json:"owner_id"`
		MemberCount int64  `json:"member_count"`
		MaxMembers  int    `json:"max_members"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		fatalf("group-info: bad response %s", short(data))
	}
	fmt.Printf("group_id=%s name=%q owner=%s members=%d/%d\n",
		out.GroupID, out.Name, out.OwnerID, out.MemberCount, out.MaxMembers)
}

func actGroupMembers() {
	sess := ensureLogin()
	id := needGroup()
	data, err := getJSON(sess.Server, groupPath(id, "/members"), sess.AccessToken)
	if err != nil {
		fatalf("group-members: %v", err)
	}
	var members []struct {
		UserID   string `json:"user_id"`
		Username string `json:"username"`
		Role     string `json:"role"`
		Muted    bool   `json:"muted"`
	}
	if err := json.Unmarshal(data, &members); err != nil {
		fatalf("group-members: bad response %s", short(data))
	}
	fmt.Printf("%-36s %-20s %-8s muted\n", "USER_ID", "USERNAME", "ROLE")
	for _, m := range members {
		fmt.Printf("%-36s %-20s %-8s %v\n", m.UserID, m.Username, m.Role, m.Muted)
	}
}

func actGroupInvite() {
	sess := ensureLogin()
	id := needGroup()
	if *fTarget == "" {
		fatalf("group-invite needs -target <uid1,uid2>")
	}
	uids := strings.Split(*fTarget, ",")
	for i := range uids {
		uids[i] = strings.TrimSpace(uids[i])
	}
	data, err := postJSON(sess.Server, groupPath(id, "/invite"), map[string]interface{}{"user_ids": uids}, sess.AccessToken)
	if err != nil {
		fatalf("group-invite: %v", err)
	}
	fmt.Printf("invite ok %s\n", short(data))
}

func actGroupJoin() {
	sess := ensureLogin()
	id := needGroup()
	data, err := postJSON(sess.Server, groupPath(id, "/join"), map[string]string{}, sess.AccessToken)
	if err != nil {
		fatalf("group-join: %v", err)
	}
	fmt.Printf("join ok %s\n", short(data))
}

func actGroupLeave() {
	sess := ensureLogin()
	id := needGroup()
	data, err := postJSON(sess.Server, groupPath(id, "/leave"), map[string]string{}, sess.AccessToken)
	if err != nil {
		fatalf("group-leave: %v", err)
	}
	fmt.Printf("leave ok %s\n", short(data))
}

func actGroupDismiss() {
	sess := ensureLogin()
	id := needGroup()
	data, err := postJSON(sess.Server, groupPath(id, "/dismiss"), map[string]string{}, sess.AccessToken)
	if err != nil {
		fatalf("group-dismiss: %v", err)
	}
	fmt.Printf("dismiss ok %s\n", short(data))
}

func actGroupKick() {
	sess := ensureLogin()
	id := needGroup()
	if *fTarget == "" {
		fatalf("group-kick needs -target <uid>")
	}
	data, err := deleteJSON(sess.Server, groupPath(id, "/members/"+url.PathEscape(*fTarget)), sess.AccessToken)
	if err != nil {
		fatalf("group-kick: %v", err)
	}
	fmt.Printf("kick ok %s\n", short(data))
}

func actGroupRole() {
	sess := ensureLogin()
	id := needGroup()
	if *fTarget == "" {
		fatalf("group-role needs -target <uid> -action2 <admin|member|owner>")
	}
	if *fAction2 != "admin" && *fAction2 != "member" && *fAction2 != "owner" {
		fatalf("group-role: -action2 must be admin|member|owner")
	}
	data, err := doJSON(http.MethodPatch, sess.Server, groupPath(id, "/members/"+url.PathEscape(*fTarget)+"/role"),
		map[string]string{"role": *fAction2}, sess.AccessToken)
	if err != nil {
		fatalf("group-role: %v", err)
	}
	fmt.Printf("role ok %s\n", short(data))
}

func actGroupMute(muted bool) {
	sess := ensureLogin()
	id := needGroup()
	if *fTarget == "" {
		what := "group-mute"
		if !muted {
			what = "group-unmute"
		}
		fatalf("%s needs -target <uid>", what)
	}
	data, err := doJSON(http.MethodPatch, sess.Server, groupPath(id, "/members/"+url.PathEscape(*fTarget)+"/mute"),
		map[string]bool{"muted": muted}, sess.AccessToken)
	if err != nil {
		fatalf("group-mute: %v", err)
	}
	fmt.Printf("mute=%v ok %s\n", muted, short(data))
}

func actGroupHistory() {
	sess := ensureLogin()
	id := needGroup()
	path := groupPath(id, "/history") + "?limit=" + url.QueryEscape(fmt.Sprint(*fLimit))
	data, err := getJSON(sess.Server, path, sess.AccessToken)
	if err != nil {
		fatalf("group-history: %v", err)
	}
	var out struct {
		Messages []struct {
			Seq      int64  `json:"seq"`
			FromUID  string `json:"from_uid"`
			Content  string `json:"content"`
			StanzaID string `json:"stanza_id"`
		} `json:"messages"`
		HasMore bool `json:"has_more"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		fatalf("group-history: bad response %s", short(data))
	}
	for _, m := range out.Messages {
		fmt.Printf("seq=%d from=%s stanza=%s content=%q\n", m.Seq, m.FromUID, m.StanzaID, m.Content)
	}
	fmt.Printf("has_more=%v count=%d\n", out.HasMore, len(out.Messages))
}

func actGroupChat() {
	sess := ensureLogin()
	id := needGroup()
	if *fMsg == "" {
		fatalf("group-chat needs -group <id> -msg <text>")
	}
	w := dialWS(sess.Server, fetchTicket(sess))
	defer w.close()

	stanza := uuid.NewString()
	out := &pb.WsMessage{Type: pb.MsgType_GROUP_CHAT, ToUid: id, Content: *fMsg, StanzaId: stanza}
	fmt.Printf("-> GROUP_CHAT to=%s stanza=%s\n", id, stanza)
	if err := w.send(out); err != nil {
		fatalf("send: %v", err)
	}
	ack, err := awaitACK(w, stanza, ackTimeout)
	if err != nil {
		fatalf("no ACK: %v", err)
	}
	fmt.Printf("ACK seq=%d stanza=%s\n", ack.GetSeq(), ack.GetStanzaId())

	if *fListen > 0 {
		fmt.Printf("listening for %s ...\n", *fListen)
		deadline := time.Now().Add(*fListen)
		for {
			rest := time.Until(deadline)
			if rest <= 0 {
				break
			}
			if _, err := w.next(rest); err != nil {
				break // timeout ends the window; closed conn aborts early
			}
		}
	}
	fmt.Println("done")
}

func actSignalSend() {
	sess := ensureLogin()
	if *fTo == "" {
		fatalf("signal-send needs -to <uid> -action2 <offer|answer|candidate> -msg <payload-text>")
	}
	var typ pb.MsgType
	switch *fAction2 {
	case "offer":
		typ = pb.MsgType_SIGNALING_OFFER
	case "answer":
		typ = pb.MsgType_SIGNALING_ANSWER
	case "candidate":
		typ = pb.MsgType_SIGNALING_CANDIDATE
	default:
		fatalf("signal-send: -action2 must be offer|answer|candidate")
	}
	w := dialWS(sess.Server, fetchTicket(sess))
	defer w.close()

	stanza := uuid.NewString()
	out := &pb.WsMessage{Type: typ, ToUid: *fTo, Payload: []byte(*fMsg), StanzaId: stanza}
	fmt.Printf("-> %s to=%s stanza=%s payload=%q\n", typ, *fTo, stanza, *fMsg)
	if err := w.send(out); err != nil {
		fatalf("send: %v", err)
	}
	if *fListen > 0 {
		fmt.Printf("listening for %s ...\n", *fListen)
		deadline := time.Now().Add(*fListen)
		for {
			rest := time.Until(deadline)
			if rest <= 0 {
				break
			}
			if _, err := w.next(rest); err != nil {
				break // timeout ends the window; closed conn aborts early
			}
		}
	}
	fmt.Println("done")
}

func actSignalListen() {
	sess := ensureLogin()
	w := dialWS(sess.Server, fetchTicket(sess))
	defer w.close()

	window := *fListen
	if window <= 0 {
		window = 30 * time.Second
	}
	fmt.Printf("listening for %s ...\n", window)
	deadline := time.Now().Add(window)
	for {
		rest := time.Until(deadline)
		if rest <= 0 {
			break
		}
		if _, err := w.next(rest); err != nil {
			break // timeout ends the window; closed conn aborts early
		}
	}
	fmt.Println("done")
}

func usage() {
	fmt.Fprintf(os.Stderr, `imcli — Stage-1+2+4 verification client
Usage: imcli -action <name> [flags]
  actions: register | login | ticket | chat | dedup-test | e2e
           friend-apply | friend-respond | friend-list | friend-pending | friend-delete
           media-upload | admin-ban | admin-kick | admin-broadcast
           group-create | group-list | group-info | group-members | group-invite
           group-join | group-leave | group-dismiss | group-kick | group-role
           group-mute | group-unmute | group-history | group-chat
           signal-send | signal-listen
  flags: -server (default http://127.0.0.1:8080) -user -pass
         -device (default interactive) -devname -msg -to -listen (default 5s)
         -target (friend-apply username; friend-respond/friend-delete user_id; admin-ban user_id;
                  group-invite uid1,uid2; group-kick/group-role/group-mute user_id)
         -remark (friend-apply) -action2 accept|reject (friend-respond, default accept);
                  admin|member|owner (group-role); offer|answer|candidate (signal-send)
         -file -mediatype (default avatar) -access (default public) (media-upload)
         -session (admin-kick) -msg (admin-broadcast content; group-create name;
                  group-chat text; signal-send payload)
         -group (group id for group-* actions) -limit (group-history page size, default 50)
         -to (signal-send peer user_id) -listen (group-chat/signal-send/signal-listen window)
  note: friend/media/group actions reuse the login session cache; admin actions need
        an admin login (e.g. -user superadmin).
`)
}

func main() {
	flag.Parse()
	if *fAction == "" {
		usage()
		os.Exit(2)
	}
	switch *fAction {
	case "register":
		if *fUser == "" || *fPass == "" {
			fatalf("register needs -user and -pass")
		}
		doRegister(*fServer, *fUser, *fPass)
	case "login":
		if *fUser == "" || *fPass == "" {
			fatalf("login needs -user and -pass")
		}
		doLogin(*fServer, *fUser, *fPass, *fDevice, *fDevName)
	case "ticket":
		sess := ensureLogin()
		fmt.Printf("ticket=%s\n", fetchTicket(sess))
	case "chat":
		actChat()
	case "dedup-test":
		actDedupTest()
	case "e2e":
		actE2E()
	case "friend-apply":
		actFriendApply()
	case "friend-respond":
		actFriendRespond()
	case "friend-list":
		actFriendList()
	case "friend-pending":
		actFriendPending()
	case "friend-delete":
		actFriendDelete()
	case "media-upload":
		actMediaUpload()
	case "admin-ban":
		actAdminBan()
	case "admin-kick":
		actAdminKick()
	case "admin-broadcast":
		actAdminBroadcast()
	case "group-create":
		actGroupCreate()
	case "group-list":
		actGroupList()
	case "group-info":
		actGroupInfo()
	case "group-members":
		actGroupMembers()
	case "group-invite":
		actGroupInvite()
	case "group-join":
		actGroupJoin()
	case "group-leave":
		actGroupLeave()
	case "group-dismiss":
		actGroupDismiss()
	case "group-kick":
		actGroupKick()
	case "group-role":
		actGroupRole()
	case "group-mute":
		actGroupMute(true)
	case "group-unmute":
		actGroupMute(false)
	case "group-history":
		actGroupHistory()
	case "group-chat":
		actGroupChat()
	case "signal-send":
		actSignalSend()
	case "signal-listen":
		actSignalListen()
	default:
		usage()
		fatalf("unknown action %q", *fAction)
	}
}
