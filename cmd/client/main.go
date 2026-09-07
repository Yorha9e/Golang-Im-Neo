// Command imcli is the Stage-1+2 command-line verification client
// (MISSIONS M5 + M4).
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
	fAction  = flag.String("action", "", "action: register|login|ticket|chat|dedup-test|e2e|friend-apply|friend-respond|friend-list|friend-pending|friend-delete|media-upload|admin-ban|admin-kick|admin-broadcast")
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

func usage() {
	fmt.Fprintf(os.Stderr, `imcli — Stage-1+2 verification client
Usage: imcli -action <name> [flags]
  actions: register | login | ticket | chat | dedup-test | e2e
           friend-apply | friend-respond | friend-list | friend-pending | friend-delete
           media-upload | admin-ban | admin-kick | admin-broadcast
  flags: -server (default http://127.0.0.1:8080) -user -pass
         -device (default interactive) -devname -msg -to -listen (default 5s)
         -target (friend-apply username; friend-respond/friend-delete user_id; admin-ban user_id)
         -remark (friend-apply) -action2 accept|reject (friend-respond, default accept)
         -file -mediatype (default avatar) -access (default public) (media-upload)
         -session (admin-kick) -msg (admin-broadcast content)
  note: friend/media actions reuse the login session cache; admin actions need
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
	default:
		usage()
		fatalf("unknown action %q", *fAction)
	}
}
