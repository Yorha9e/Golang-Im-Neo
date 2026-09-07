// Stage-2 DoD proof (MISSION M4): friend lifecycle + WS notify, media
// upload/access isolation, admin broadcast/ban/precise-kick — all against the
// fully-wired app (app.Build + httptest server), reusing the Stage-1 helpers
// in e2e_test.go (same package app_test).
package app_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	pb "golang-im-neo-system/proto"

	"golang-im-neo-system/internal/app"
)

// ---------- Stage-2 local helpers (s2* prefix avoids clashing with e2e_test.go) ----------

type s2Cred struct {
	userID, at, sessionID string
}

// s2LoginFull logs in with an explicit device class/name and returns the
// session id too (needed for the precise-kick DoD).
func s2LoginFull(t *testing.T, base, user, pass, deviceClass, deviceName string) s2Cred {
	t.Helper()
	data := e2ePOST(t, base, "/api/v1/auth/login", map[string]string{
		"username": user, "password": pass,
		"device_class": deviceClass, "device_name": deviceName,
	}, "")
	var out struct {
		AccessToken string `json:"access_token"`
		UserID      string `json:"user_id"`
		SessionID   string `json:"session_id"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.AccessToken == "" || out.SessionID == "" {
		t.Fatalf("login %s/%s: bad data %s", user, deviceClass, short(data))
	}
	return s2Cred{userID: out.UserID, at: out.AccessToken, sessionID: out.SessionID}
}

// s2DoRaw issues one JSON request and returns the raw HTTP status + envelope
// without failing on business-error codes (for negative assertions).
func s2DoRaw(t *testing.T, method, base, path string, body interface{}, token string) (int, e2eEnv) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
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
	resp, err := e2eHTTP.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var env e2eEnv
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("%s %s: bad envelope %q: %v", method, path, short(raw), err)
	}
	return resp.StatusCode, env
}

func s2GETRaw(t *testing.T, base, path, token string) (int, e2eEnv) {
	t.Helper()
	return s2DoRaw(t, http.MethodGet, base, path, nil, token)
}

func s2DELETERaw(t *testing.T, base, path, token string) (int, e2eEnv) {
	t.Helper()
	return s2DoRaw(t, http.MethodDelete, base, path, nil, token)
}

type s2UploadData struct {
	MID       string `json:"mid"`
	AccessURL string `json:"access_url"`
}

// s2Upload POSTs a multipart file to /api/v1/media/upload.
func s2Upload(t *testing.T, base, token, filename, mediaType, accessLevel string, data []byte) (int, e2eEnv, s2UploadData) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write part: %v", err)
	}
	_ = w.WriteField("media_type", mediaType)
	_ = w.WriteField("access_level", accessLevel)
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/media/upload", &buf)
	if err != nil {
		t.Fatalf("build req: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e2eHTTP.Do(req)
	if err != nil {
		t.Fatalf("POST upload: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var env e2eEnv
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("POST upload: bad envelope %q: %v", short(raw), err)
	}
	var ud s2UploadData
	if env.Code == 0 && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, &ud); err != nil {
			t.Fatalf("upload data decode: %v (%s)", err, short(env.Data))
		}
	}
	return resp.StatusCode, env, ud
}

// s2GETBytes issues a raw GET (optional token) for media download assertions.
func s2GETBytes(t *testing.T, base, path, token string) (int, []byte, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatalf("build req: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e2eHTTP.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, b, resp.Header.Get("Content-Type")
}

// s2WaitClosed waits until the WS read loop observes the server-side close
// (channel closed), i.e. the client was kicked/banned.
func s2WaitClosed(t *testing.T, w *e2eWS, what string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		rest := time.Until(deadline)
		if rest <= 0 {
			t.Fatalf("%s: WS still open after %s", what, timeout)
		}
		select {
		case _, ok := <-w.ch:
			if !ok {
				return
			}
			// Buffered frame arrived first; keep waiting for the close.
		case <-time.After(rest):
			t.Fatalf("%s: WS still open after %s", what, timeout)
		}
	}
}

// s2AwaitNotice waits for a SYSTEM_NOTICE with the given business code.
func s2AwaitNotice(t *testing.T, w *e2eWS, what string, code int) *pb.WsMessage {
	t.Helper()
	return w.await(t, what, func(m *pb.WsMessage) bool {
		if m.GetType() != pb.MsgType_SYSTEM_NOTICE {
			return false
		}
		var e struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal([]byte(m.GetExtra()), &e); err != nil {
			return false
		}
		return e.Code == code
	})
}

// 1x1 transparent PNG bytes (valid image payload for avatar uploads).
var s2PNG1x1 = func() []byte {
	b, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err != nil {
		panic(err)
	}
	return b
}()

// ---------- the Stage-2 DoD test ----------

func TestStage2EndToEnd(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "e2e-s2.db")
	cfg := e2eConfig(t, dbPath)
	cfg.Media.StorageDir = filepath.Join(t.TempDir(), "media")

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

	// Seeded superadmin login for admin calls.
	admin := s2LoginFull(t, base, "superadmin", "SuperAdmin123!", "interactive", "admin-e2e")

	// ----- Friend lifecycle + WS notify -----
	alice := e2eRegisterLogin(t, base, "alice")
	bob := e2eRegisterLogin(t, base, "bob")
	carol := e2eRegisterLogin(t, base, "carol")
	dave := e2eRegisterLogin(t, base, "dave")
	wa := e2eDial(t, base, e2eTicket(t, base, alice.at))
	wb := e2eDial(t, base, e2eTicket(t, base, bob.at))
	wc := e2eDial(t, base, e2eTicket(t, base, carol.at))
	wd := e2eDial(t, base, e2eTicket(t, base, dave.at))
	waitHubCount(t, a, 4)

	// alice applies to bob (by username) → bob gets FRIEND_APPLY_NOTIFY.
	data := e2ePOST(t, base, "/api/v1/friends/apply",
		map[string]string{"target_username": "bob", "remark": "hi"}, alice.at)
	var ap struct {
		TargetUserID string `json:"target_user_id"`
	}
	if err := json.Unmarshal(data, &ap); err != nil || ap.TargetUserID != bob.userID {
		t.Fatalf("apply data = %s, want target %s", short(data), bob.userID)
	}
	applyN := wb.await(t, "bob FRIEND_APPLY_NOTIFY", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_FRIEND_APPLY_NOTIFY && m.GetFromUid() == alice.userID
	})
	_ = applyN

	// bob accepts (initiator = alice) → alice gets FRIEND_ACCEPT_NOTIFY.
	e2ePOST(t, base, "/api/v1/friends/respond",
		map[string]string{"target_user_id": alice.userID, "action": "accept"}, bob.at)
	wa.await(t, "alice FRIEND_ACCEPT_NOTIFY", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_FRIEND_ACCEPT_NOTIFY && m.GetFromUid() == bob.userID
	})

	// Friendship allows alice→bob PRIVATE_CHAT delivery.
	sPriv := uuid.NewString()
	wa.send(t, &pb.WsMessage{Type: pb.MsgType_PRIVATE_CHAT, ToUid: bob.userID, Content: "hi bob", StanzaId: sPriv})
	ackPriv := wa.await(t, "alice ACK private", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_ACK && m.GetStanzaId() == sPriv
	})
	if ackPriv.GetSeq() <= 0 {
		t.Fatalf("private ACK bad seq %d", ackPriv.GetSeq())
	}
	gotPriv := wb.await(t, "bob PRIVATE delivery", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_PRIVATE_CHAT && m.GetStanzaId() == sPriv
	})
	if gotPriv.GetFromUid() != alice.userID || gotPriv.GetContent() != "hi bob" {
		t.Fatalf("bob private wrong: %v", gotPriv)
	}

	// carol applies to dave, dave rejects → not friends: carol→dave blocked 30001.
	e2ePOST(t, base, "/api/v1/friends/apply",
		map[string]string{"target_username": "dave"}, carol.at)
	wd.await(t, "dave FRIEND_APPLY_NOTIFY", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_FRIEND_APPLY_NOTIFY && m.GetFromUid() == carol.userID
	})
	e2ePOST(t, base, "/api/v1/friends/respond",
		map[string]string{"target_user_id": carol.userID, "action": "reject"}, dave.at)
	wc.send(t, &pb.WsMessage{Type: pb.MsgType_PRIVATE_CHAT, ToUid: dave.userID, Content: "yo", StanzaId: uuid.NewString()})
	if n := s2AwaitNotice(t, wc, "carol 30001 after reject", 30001); noticeCode(t, n) != 30001 {
		t.Fatalf("reject-path deny code = %d, want 30001", noticeCode(t, n))
	}

	// alice deletes bob → PRIVATE_CHAT now blocked with 30001, bob gets nothing.
	if code, env := s2DELETERaw(t, base, "/api/v1/friends/"+bob.userID, alice.at); code != 200 || env.Code != 0 {
		t.Fatalf("delete: http=%d env=%+v", code, env)
	}
	sBK := uuid.NewString()
	wa.send(t, &pb.WsMessage{Type: pb.MsgType_PRIVATE_CHAT, ToUid: bob.userID, Content: "still there?", StanzaId: sBK})
	s2AwaitNotice(t, wa, "alice 30001 after delete", 30001)
	if rest := wb.drain(negWindow); len(rest) != 0 {
		t.Fatalf("bob should receive NOTHING after delete, got %d frames", len(rest))
	}

	// Revival: alice re-applies after delete → succeeds, bob gets a fresh notify.
	e2ePOST(t, base, "/api/v1/friends/apply",
		map[string]string{"target_username": "bob", "remark": "again"}, alice.at)
	wb.await(t, "bob fresh FRIEND_APPLY_NOTIFY", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_FRIEND_APPLY_NOTIFY && m.GetFromUid() == alice.userID
	})

	// ----- Media -----
	httpCode, env, ud := s2Upload(t, base, alice.at, "ava.png", "avatar", "public", s2PNG1x1)
	if httpCode != 200 || env.Code != 0 || ud.MID == "" || ud.AccessURL != "/api/v1/media/"+ud.MID {
		t.Fatalf("avatar upload: http=%d env=%+v data=%+v", httpCode, env, ud)
	}
	if code, body, _ := s2GETBytes(t, base, ud.AccessURL, ""); code != 200 || !bytes.Equal(body, s2PNG1x1) {
		t.Fatalf("public anon GET: http=%d bytes=%d, want 200 + %d bytes", code, len(body), len(s2PNG1x1))
	}

	// Oversize avatar (>2MB) → 40001.
	big := bytes.Repeat([]byte("x"), (2<<20)+1)
	if code, env, _ := s2Upload(t, base, alice.at, "big.png", "avatar", "public", big); env.Code != 40001 || code == 200 {
		t.Fatalf("oversize avatar: http=%d env=%+v, want 40001 non-200", code, env)
	}
	// Disallowed ext → 40002.
	if code, env, _ := s2Upload(t, base, alice.at, "evil.exe", "avatar", "public", []byte("MZ-fake")); env.Code != 40002 || code == 200 {
		t.Fatalf("bad ext avatar: http=%d env=%+v, want 40002 non-200", code, env)
	}

	// Private image isolation (distinct bytes to avoid same-uploader dedup).
	privBytes := []byte("private-image-payload-s2-001")
	_, env, pud := s2Upload(t, base, alice.at, "secret.png", "image", "private", privBytes)
	if env.Code != 0 || pud.MID == "" {
		t.Fatalf("private upload: env=%+v", env)
	}
	if code, body, _ := s2GETBytes(t, base, pud.AccessURL, ""); code != 401 {
		t.Fatalf("private anon GET: http=%d body=%q, want 401", code, short(body))
	} else {
		var anonEnv e2eEnv
		if err := json.Unmarshal(body, &anonEnv); err != nil || anonEnv.Code != 10002 {
			t.Fatalf("private anon GET env: %v %+v, want 10002", err, anonEnv)
		}
	}
	if code, body, _ := s2GETBytes(t, base, pud.AccessURL, bob.at); code != 403 {
		t.Fatalf("private bob GET: http=%d body=%q, want 403", code, short(body))
	} else {
		var otherEnv e2eEnv
		if err := json.Unmarshal(body, &otherEnv); err != nil || otherEnv.Code != 10003 {
			t.Fatalf("private bob GET env: %v %+v, want 10003", err, otherEnv)
		}
	}
	if code, body, _ := s2GETBytes(t, base, pud.AccessURL, alice.at); code != 200 || !bytes.Equal(body, privBytes) {
		t.Fatalf("private owner GET: http=%d bytes=%d, want 200 + payload", code, len(body))
	}

	// ----- Admin broadcast: every connected client gets the SYSTEM_NOTICE -----
	bcast := "s2-notice-" + uuid.NewString()
	e2ePOST(t, base, "/api/v1/admin/broadcast", map[string]string{"content": bcast}, admin.at)
	for _, cw := range []*e2eWS{wa, wb, wc, wd} {
		got := cw.await(t, "broadcast SYSTEM_NOTICE", func(m *pb.WsMessage) bool {
			return m.GetType() == pb.MsgType_SYSTEM_NOTICE && m.GetContent() == bcast
		})
		if got.GetContent() != bcast {
			t.Fatalf("broadcast content = %q, want %q", got.GetContent(), bcast)
		}
	}

	// ----- Admin ban: bob drops promptly and cannot reconnect -----
	e2ePOST(t, base, "/api/v1/admin/users/"+bob.userID+"/ban", map[string]string{}, admin.at)
	s2WaitClosed(t, wb, "bob WS after ban", 2*time.Second)
	waitHubCount(t, a, 3)

	// Old AT is dead: ticket denied (token_version bumped → 10002; the JWT
	// middleware's banned-status check may surface first as 20004 — either
	// proves the token can no longer connect).
	if code, env := s2DoRaw(t, http.MethodPost, base, "/api/v1/auth/ticket", map[string]string{}, bob.at); code == 200 || (env.Code != 10002 && env.Code != 20004) {
		t.Fatalf("banned ticket: http=%d env=%+v, want 401/10002 or 403/20004", code, env)
	}
	// Re-login refused: 403/20004.
	if code, env := s2DoRaw(t, http.MethodPost, base, "/api/v1/auth/login",
		map[string]string{"username": "bob", "password": e2ePass, "device_class": "interactive", "device_name": "e2e"}, ""); code != 403 || env.Code != 20004 {
		t.Fatalf("banned re-login: http=%d env=%+v, want 403/20004", code, env)
	}
	// Old AT on an HTTP route is dead too.
	if code, env := s2GETRaw(t, base, "/api/v1/friends", bob.at); code == 200 || (env.Code != 10002 && env.Code != 20004) {
		t.Fatalf("banned GET /friends: http=%d env=%+v, want 401/10002 or 403/20004", code, env)
	}

	// ----- Admin precise kick: carol desktop drops, ESP32 hardware stays -----
	desk := s2LoginFull(t, base, "carol", e2ePass, "interactive", "desktop-e2e")
	hw := s2LoginFull(t, base, "carol", e2ePass, "hardware", "esp32-e2e")
	wDesk := e2eDial(t, base, e2eTicket(t, base, desk.at))
	wHW := e2eDial(t, base, e2eTicket(t, base, hw.at))
	waitHubCount(t, a, 5)

	e2ePOST(t, base, "/api/v1/admin/sessions/"+desk.sessionID+"/kick", map[string]string{}, admin.at)
	s2WaitClosed(t, wDesk, "carol desktop WS after kick", 2*time.Second)
	waitHubCount(t, a, 4)

	// Kicked desktop session token is revoked; hardware token still works.
	if code, env := s2GETRaw(t, base, "/api/v1/friends", desk.at); code != 401 || (env.Code != 10002) {
		t.Fatalf("kicked desktop GET: http=%d env=%+v, want 401/10002", code, env)
	}
	if code, env := s2GETRaw(t, base, "/api/v1/friends", hw.at); code != 200 || env.Code != 0 {
		t.Fatalf("hardware GET: http=%d env=%+v, want 200", code, env)
	}

	// Hardware session still receives broadcasts (heartbeat path also viable).
	bcast2 := "s2-postkick-" + uuid.NewString()
	// Drain any in-flight broadcast echo? Not needed: unique content predicate.
	e2ePOST(t, base, "/api/v1/admin/broadcast", map[string]string{"content": bcast2}, admin.at)
	wHW.await(t, "hardware post-kick broadcast", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_SYSTEM_NOTICE && m.GetContent() == bcast2
	})
	// ... and the surviving pre-kick clients get it too.
	wa.await(t, "alice post-kick broadcast", func(m *pb.WsMessage) bool {
		return m.GetType() == pb.MsgType_SYSTEM_NOTICE && m.GetContent() == bcast2
	})
}
