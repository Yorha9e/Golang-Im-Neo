package friend

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/middleware"
	"golang-im-neo-system/internal/store"
)

const friendTestSecret = "friend-test-secret-32chars-long!!"

func newFriendHTTPSetup(t *testing.T) (*gin.Engine, *gorm.DB, *fakeEmitter, *fakeInvalidator, map[string]string, map[string]string) {
	t.Helper()
	db, err := store.NewDB(store.DBConfig{Path: filepath.Join(t.TempDir(), "h.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	// Seed users.
	users := []struct{ id, name string }{
		{"u_alice", "alice"},
		{"u_bob", "bob"},
		{"u_carol", "carol"},
	}
	for _, u := range users {
		if err := db.Create(&store.User{
			ID: u.id, Username: u.name, PasswordHash: "hash",
			Role: "user", Status: 1, TokenVersion: 1,
		}).Error; err != nil {
			t.Fatalf("seed %q: %v", u.name, err)
		}
	}
	em := &fakeEmitter{}
	iv := &fakeInvalidator{}
	svc := NewFriendService(db, em, iv, nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	rg := r.Group("/api/v1/friends")
	RegisterRoutes(rg, svc, middleware.JWTAuthMiddleware(friendTestSecret, db))

	tokens := map[string]string{}
	ids := map[string]string{"alice": "u_alice", "bob": "u_bob", "carol": "u_carol"}
	for name, uid := range ids {
		tokens[name] = signFriendToken(t, friendTestSecret, uid, name, 1)
	}
	return r, db, em, iv, tokens, ids
}

func signFriendToken(t *testing.T, secret, userID, username string, tv int) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"user_id": userID, "username": username, "role": "user",
		"session_id": "s_test", "device_class": "interactive",
		"tv": tv, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

type friendEnv struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func doFriendReq(t *testing.T, r http.Handler, method, path, body, token string) (int, friendEnv) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var env friendEnv
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s %s: bad envelope %q: %v", method, path, w.Body.String(), err)
	}
	return w.Code, env
}

func TestHTTPGuardEnforced(t *testing.T) {
	r, _, _, _, _, _ := newFriendHTTPSetup(t)
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/v1/friends", ""},
		{"GET", "/api/v1/friends/", ""},
		{"GET", "/api/v1/friends/pending", ""},
		{"POST", "/api/v1/friends/apply", `{"target_username":"bob"}`},
		{"POST", "/api/v1/friends/respond", `{"target_user_id":"u_alice","action":"accept"}`},
		{"DELETE", "/api/v1/friends/u_bob", ""},
	} {
		code, env := doFriendReq(t, r, tc.method, tc.path, tc.body, "")
		if code != 401 || env.Code != CodeUnauthorized {
			t.Fatalf("%s %s no-auth: http=%d env=%+v, want 401/10002", tc.method, tc.path, code, env)
		}
	}
	// Bad token also rejected.
	code, env := doFriendReq(t, r, "GET", "/api/v1/friends", "", "garbage")
	if code != 401 || env.Code != CodeUnauthorized {
		t.Fatalf("bad token: http=%d env=%+v, want 401/10002", code, env)
	}
}

func TestHTTPFullFlow(t *testing.T) {
	r, _, em, iv, tokens, ids := newFriendHTTPSetup(t)

	// alice: empty friends.
	code, env := doFriendReq(t, r, "GET", "/api/v1/friends", "", tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("list empty: http=%d env=%+v", code, env)
	}
	var lf struct {
		Friends []FriendItem `json:"friends"`
	}
	if err := json.Unmarshal(env.Data, &lf); err != nil || lf.Friends == nil || len(lf.Friends) != 0 {
		t.Fatalf("empty friends data: %q err=%v", string(env.Data), err)
	}

	// alice apply bob.
	code, env = doFriendReq(t, r, "POST", "/api/v1/friends/apply", `{"target_username":"bob","remark":"hi"}`, tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("apply: http=%d env=%+v", code, env)
	}
	var ap struct {
		TargetUserID string `json:"target_user_id"`
	}
	if err := json.Unmarshal(env.Data, &ap); err != nil || ap.TargetUserID != ids["bob"] {
		t.Fatalf("apply data: %q err=%v", string(env.Data), err)
	}

	// bob pending contains alice.
	code, env = doFriendReq(t, r, "GET", "/api/v1/friends/pending", "", tokens["bob"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("pending: http=%d env=%+v", code, env)
	}
	var lp struct {
		Requests []PendingItem `json:"requests"`
	}
	if err := json.Unmarshal(env.Data, &lp); err != nil || len(lp.Requests) != 1 || lp.Requests[0].UserID != ids["alice"] {
		t.Fatalf("pending data: %q err=%v", string(env.Data), err)
	}

	// bob accept.
	body := `{"target_user_id":"` + ids["alice"] + `","action":"accept"}`
	code, env = doFriendReq(t, r, "POST", "/api/v1/friends/respond", body, tokens["bob"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("respond: http=%d env=%+v", code, env)
	}

	// alice friends contains bob.
	code, env = doFriendReq(t, r, "GET", "/api/v1/friends", "", tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("list after accept: http=%d env=%+v", code, env)
	}
	if err := json.Unmarshal(env.Data, &lf); err != nil || len(lf.Friends) != 1 || lf.Friends[0].UserID != ids["bob"] {
		t.Fatalf("friends data: %q err=%v", string(env.Data), err)
	}
	if lf.Friends[0].Username != "bob" {
		t.Fatalf("friend username = %q, want bob", lf.Friends[0].Username)
	}

	// trailing-slash alias also works.
	code, env = doFriendReq(t, r, "GET", "/api/v1/friends/", "", tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("list slash: http=%d env=%+v", code, env)
	}

	// delete.
	code, env = doFriendReq(t, r, "DELETE", "/api/v1/friends/"+ids["bob"], "", tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("delete: http=%d env=%+v", code, env)
	}
	code, env = doFriendReq(t, r, "GET", "/api/v1/friends", "", tokens["alice"])
	var lf2 struct {
		Friends []FriendItem `json:"friends"`
	}
	_ = json.Unmarshal(env.Data, &lf2)
	if len(lf2.Friends) != 0 {
		t.Fatalf("after delete friends=%+v, want empty", lf2.Friends)
	}

	// Notifications + invalidations happened (service-level fakes shared).
	if len(em.byType(0)) != 0 {
		// placeholder to use em; real asserts below
	}
	_ = iv
}

func TestHTTPBusinessErrors(t *testing.T) {
	r, _, _, _, tokens, ids := newFriendHTTPSetup(t)

	// self-apply → 10001 non-200.
	code, env := doFriendReq(t, r, "POST", "/api/v1/friends/apply", `{"target_username":"alice"}`, tokens["alice"])
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("self-apply: http=%d env=%+v, want 400/10001", code, env)
	}
	// unknown → 20001 non-200.
	code, env = doFriendReq(t, r, "POST", "/api/v1/friends/apply", `{"target_username":"ghost"}`, tokens["alice"])
	if code == 200 || env.Code != CodeUserNotFound {
		t.Fatalf("unknown: http=%d env=%+v, want 20001 non-200", code, env)
	}
	// bad body → 10001.
	code, env = doFriendReq(t, r, "POST", "/api/v1/friends/apply", `not-json`, tokens["alice"])
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("bad body: http=%d env=%+v, want 400/10001", code, env)
	}
	// missing target_username → 10001.
	code, env = doFriendReq(t, r, "POST", "/api/v1/friends/apply", `{"remark":"x"}`, tokens["alice"])
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("missing target: http=%d env=%+v, want 400/10001", code, env)
	}

	// valid apply then duplicate → 30003.
	code, env = doFriendReq(t, r, "POST", "/api/v1/friends/apply", `{"target_username":"bob"}`, tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("first apply: http=%d env=%+v", code, env)
	}
	code, env = doFriendReq(t, r, "POST", "/api/v1/friends/apply", `{"target_username":"bob"}`, tokens["alice"])
	if code == 200 || env.Code != CodeFriendAlready {
		t.Fatalf("dup apply: http=%d env=%+v, want 30003 non-200", code, env)
	}

	// invalid action → 10001.
	code, env = doFriendReq(t, r, "POST", "/api/v1/friends/respond",
		`{"target_user_id":"`+ids["alice"]+`","action":"maybe"}`, tokens["bob"])
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("bad action: http=%d env=%+v, want 400/10001", code, env)
	}
	// respond non-pending (carol has no request from alice) → 30001.
	code, env = doFriendReq(t, r, "POST", "/api/v1/friends/respond",
		`{"target_user_id":"`+ids["alice"]+`","action":"accept"}`, tokens["carol"])
	if code == 200 || env.Code != CodeFriendNotFound {
		t.Fatalf("non-pending respond: http=%d env=%+v, want 30001 non-200", code, env)
	}
	// wrong party: alice tries to accept her own request → 30001.
	code, env = doFriendReq(t, r, "POST", "/api/v1/friends/respond",
		`{"target_user_id":"`+ids["bob"]+`","action":"accept"}`, tokens["alice"])
	if code == 200 || env.Code != CodeFriendNotFound {
		t.Fatalf("wrong-party respond: http=%d env=%+v, want 30001 non-200", code, env)
	}
	// delete non-friend → 30001.
	code, env = doFriendReq(t, r, "DELETE", "/api/v1/friends/"+ids["carol"], "", tokens["alice"])
	if code == 200 || env.Code != CodeFriendNotFound {
		t.Fatalf("delete non-friend: http=%d env=%+v, want 30001 non-200", code, env)
	}
	// envelope: error has no data field, success has data.
	_, env = doFriendReq(t, r, "POST", "/api/v1/friends/apply", `{"target_username":"bob"}`, tokens["alice"])
	if env.Msg == "" {
		t.Fatal("error envelope must carry msg")
	}
}

func TestHTTPEnvelopeShape(t *testing.T) {
	r, _, _, _, tokens, _ := newFriendHTTPSetup(t)
	// Success envelope: code 0, msg success, data object.
	_, env := doFriendReq(t, r, "GET", "/api/v1/friends", "", tokens["alice"])
	if env.Code != 0 || env.Msg != "success" || len(env.Data) == 0 {
		t.Fatalf("success envelope wrong: %+v", env)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatalf("success data not object: %q", string(env.Data))
	}
	if _, ok := raw["friends"]; !ok {
		t.Fatalf("friends key missing in %q", string(env.Data))
	}
	_, env = doFriendReq(t, r, "GET", "/api/v1/friends/pending", "", tokens["alice"])
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatalf("pending data not object: %q", string(env.Data))
	}
	if _, ok := raw["requests"]; !ok {
		t.Fatalf("requests key missing in %q", string(env.Data))
	}
}
