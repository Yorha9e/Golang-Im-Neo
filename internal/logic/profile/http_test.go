package profile

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

const profileTestSecret = "profile-test-secret-32chars-long!"

func newProfileHTTPSetup(t *testing.T) (*gin.Engine, *gorm.DB, map[string]string, map[string]string) {
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
	for _, u := range []struct{ id, name string }{
		{"u_alice", "alice"},
		{"u_bob", "bob"},
	} {
		if err := db.Create(&store.User{
			ID: u.id, Username: u.name, PasswordHash: "hash",
			Role: "user", Status: 1, TokenVersion: 1,
		}).Error; err != nil {
			t.Fatalf("seed %q: %v", u.name, err)
		}
	}
	svc := NewProfileService(db, NoopResolver{}, nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	profileRG := r.Group("/api/v1/profile")
	postsCardRG := r.Group("/api/v1/posts")
	// Must not panic: GET /:username and the posts routes share no method tree collision.
	RegisterRoutes(profileRG, postsCardRG, svc, middleware.JWTAuthMiddleware(profileTestSecret, db))

	ids := map[string]string{"alice": "u_alice", "bob": "u_bob"}
	tokens := map[string]string{}
	for name, uid := range ids {
		tokens[name] = signProfileToken(t, profileTestSecret, uid, name, 1)
	}
	return r, db, tokens, ids
}

func signProfileToken(t *testing.T, secret, userID, username string, tv int) string {
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

type profileEnv struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func doProfileReq(t *testing.T, r http.Handler, method, path, body, token string) (int, profileEnv) {
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
	var env profileEnv
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s %s: bad envelope %q: %v", method, path, w.Body.String(), err)
	}
	return w.Code, env
}

func TestHTTPRoutesRegisterWithoutPanic(t *testing.T) {
	r, _, _, _ := newProfileHTTPSetup(t)
	paths := map[string]bool{}
	for _, rt := range r.Routes() {
		paths[rt.Method+" "+rt.Path] = true
	}
	for _, want := range []string{
		"GET /api/v1/profile/:username",
		"PUT /api/v1/profile",
		"PUT /api/v1/profile/",
		"POST /api/v1/profile/posts",
		"DELETE /api/v1/profile/posts/:post_id",
		"GET /api/v1/posts/:post_id/card",
	} {
		if !paths[want] {
			t.Fatalf("route %q not registered (got %v)", want, paths)
		}
	}
}

func TestHTTPGuardEnforced(t *testing.T) {
	r, _, _, _ := newProfileHTTPSetup(t)
	for _, tc := range []struct{ method, path, body string }{
		{"PUT", "/api/v1/profile", `{"nickname":"x"}`},
		{"PUT", "/api/v1/profile/", `{"nickname":"x"}`},
		{"POST", "/api/v1/profile/posts", `{"media_type":"text","content":"hi"}`},
		{"DELETE", "/api/v1/profile/posts/1", ""},
	} {
		code, env := doProfileReq(t, r, tc.method, tc.path, tc.body, "")
		if code != 401 || env.Code != CodeUnauthorized {
			t.Fatalf("%s %s no-auth: http=%d env=%+v, want 401/10002", tc.method, tc.path, code, env)
		}
	}
	// Bad token also rejected on guarded routes.
	code, env := doProfileReq(t, r, "POST", "/api/v1/profile/posts", `{"media_type":"text","content":"hi"}`, "garbage")
	if code != 401 || env.Code != CodeUnauthorized {
		t.Fatalf("bad token: http=%d env=%+v, want 401/10002", code, env)
	}
	// Public routes do NOT require auth: unknown user → 20001/404 (not 401).
	code, env = doProfileReq(t, r, "GET", "/api/v1/profile/ghost", "", "")
	if code != 404 || env.Code != CodeUserNotFound {
		t.Fatalf("public profile no-auth: http=%d env=%+v, want 404/20001", code, env)
	}
	// Known user without token → 200 (public).
	code, env = doProfileReq(t, r, "GET", "/api/v1/profile/alice", "", "")
	if code != 200 || env.Code != 0 {
		t.Fatalf("public profile alice: http=%d env=%+v, want 200/0", code, env)
	}
	// Public card without token → 40004/404 for missing post (not 401).
	code, env = doProfileReq(t, r, "GET", "/api/v1/posts/999999/card", "", "")
	if code != 404 || env.Code != CodePostNotFound {
		t.Fatalf("public card no-auth: http=%d env=%+v, want 404/40004", code, env)
	}
	// Invalid card id without token → 10001/400 (still not 401).
	code, env = doProfileReq(t, r, "GET", "/api/v1/posts/abc/card", "", "")
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("bad card id: http=%d env=%+v, want 400/10001", code, env)
	}
}

func TestHTTPFullFlow(t *testing.T) {
	r, _, tokens, _ := newProfileHTTPSetup(t)

	// Update nickname.
	code, env := doProfileReq(t, r, "PUT", "/api/v1/profile", `{"nickname":"Ali"}`, tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("update: http=%d env=%+v", code, env)
	}
	var upd struct {
		User UserCard `json:"user"`
	}
	if err := json.Unmarshal(env.Data, &upd); err != nil || upd.User.Nickname != "Ali" || upd.User.Username != "alice" {
		t.Fatalf("update data: %q err=%v", string(env.Data), err)
	}

	// Create text post.
	code, env = doProfileReq(t, r, "POST", "/api/v1/profile/posts", `{"media_type":"text","content":"hello"}`, tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("create text: http=%d env=%+v", code, env)
	}
	var created struct {
		Post PostDTO `json:"post"`
	}
	if err := json.Unmarshal(env.Data, &created); err != nil || created.Post.ID == 0 || created.Post.Content != "hello" {
		t.Fatalf("create data: %q err=%v", string(env.Data), err)
	}
	textID := created.Post.ID

	// Create bilibili post with caller preview.
	code, env = doProfileReq(t, r, "POST", "/api/v1/profile/posts",
		`{"media_type":"bilibili","bilibili_bvid":"BV1xx411c7mD","link_preview":{"title":"T","cover_url":"http://x/c.png","target_url":"https://example.com/v","site":"example"}}`,
		tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("create bilibili: http=%d env=%+v", code, env)
	}
	var createdBV struct {
		Post PostDTO `json:"post"`
	}
	if err := json.Unmarshal(env.Data, &createdBV); err != nil || createdBV.Post.ID == 0 {
		t.Fatalf("create bv data: %q err=%v", string(env.Data), err)
	}
	if createdBV.Post.LinkPreview == nil || createdBV.Post.LinkPreview.TargetURL != "https://example.com/v" {
		t.Fatalf("create bv preview: %+v", createdBV.Post.LinkPreview)
	}
	bvID := createdBV.Post.ID

	// Public profile shows card + newest-first posts.
	code, env = doProfileReq(t, r, "GET", "/api/v1/profile/alice", "", "")
	if code != 200 || env.Code != 0 {
		t.Fatalf("public: http=%d env=%+v", code, env)
	}
	var pub struct {
		User  UserCard  `json:"user"`
		Posts []PostDTO `json:"posts"`
	}
	if err := json.Unmarshal(env.Data, &pub); err != nil {
		t.Fatalf("public data: %q err=%v", string(env.Data), err)
	}
	if pub.User.Nickname != "Ali" {
		t.Fatalf("public nickname = %q, want Ali", pub.User.Nickname)
	}
	if len(pub.Posts) != 2 || pub.Posts[0].ID != bvID || pub.Posts[1].ID != textID {
		t.Fatalf("public posts order wrong: %+v (want [%d %d] newest first)", pub.Posts, bvID, textID)
	}
	if pub.Posts[0].LinkPreview == nil || pub.Posts[0].LinkPreview.Title != "T" {
		t.Fatalf("public preview missing: %+v", pub.Posts[0])
	}

	// Card for bilibili post carries the stored preview.
	code, env = doProfileReq(t, r, "GET", "/api/v1/posts/"+itoa(bvID)+"/card", "", "")
	if code != 200 || env.Code != 0 {
		t.Fatalf("card bv: http=%d env=%+v", code, env)
	}
	var card PostCard
	if err := json.Unmarshal(env.Data, &card); err != nil || card.PostID != bvID || card.LinkPreview == nil {
		t.Fatalf("card data: %q err=%v", string(env.Data), err)
	}

	// Non-owner delete → 403/10003.
	code, env = doProfileReq(t, r, "DELETE", "/api/v1/profile/posts/"+itoa(textID), "", tokens["bob"])
	if code != 403 || env.Code != CodeForbidden {
		t.Fatalf("non-owner delete: http=%d env=%+v, want 403/10003", code, env)
	}
	// Owner delete → 200.
	code, env = doProfileReq(t, r, "DELETE", "/api/v1/profile/posts/"+itoa(textID), "", tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("owner delete: http=%d env=%+v", code, env)
	}
	// Card after delete → 404/40004.
	code, env = doProfileReq(t, r, "GET", "/api/v1/posts/"+itoa(textID)+"/card", "", "")
	if code != 404 || env.Code != CodePostNotFound {
		t.Fatalf("card after delete: http=%d env=%+v, want 404/40004", code, env)
	}
}

func TestHTTPBusinessErrors(t *testing.T) {
	r, _, tokens, _ := newProfileHTTPSetup(t)

	// Bad media_type → 400/10001.
	code, env := doProfileReq(t, r, "POST", "/api/v1/profile/posts", `{"media_type":"audio"}`, tokens["alice"])
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("bad media_type: http=%d env=%+v, want 400/10001", code, env)
	}
	// Bad BVID → 400/40003.
	code, env = doProfileReq(t, r, "POST", "/api/v1/profile/posts", `{"media_type":"bilibili","bilibili_bvid":"bad"}`, tokens["alice"])
	if code != 400 || env.Code != CodeBilibiliInvalid {
		t.Fatalf("bad bvid: http=%d env=%+v, want 400/40003", code, env)
	}
	// Text without content → 400/10001.
	code, env = doProfileReq(t, r, "POST", "/api/v1/profile/posts", `{"media_type":"text","content":"  "}`, tokens["alice"])
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("empty text: http=%d env=%+v, want 400/10001", code, env)
	}
	// Bad body → 400/10001.
	code, env = doProfileReq(t, r, "POST", "/api/v1/profile/posts", `not-json`, tokens["alice"])
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("bad body: http=%d env=%+v, want 400/10001", code, env)
	}
	code, env = doProfileReq(t, r, "PUT", "/api/v1/profile", `not-json`, tokens["alice"])
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("bad update body: http=%d env=%+v, want 400/10001", code, env)
	}
	// Over-long nickname → 400/10001.
	code, env = doProfileReq(t, r, "PUT", "/api/v1/profile", `{"nickname":"`+strings.Repeat("n", 65)+`"}`, tokens["alice"])
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("long nickname: http=%d env=%+v, want 400/10001", code, env)
	}
	// Invalid post_id → 400/10001 (delete + card).
	code, env = doProfileReq(t, r, "DELETE", "/api/v1/profile/posts/abc", "", tokens["alice"])
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("bad delete id: http=%d env=%+v, want 400/10001", code, env)
	}
	// Missing post delete → 404/40004.
	code, env = doProfileReq(t, r, "DELETE", "/api/v1/profile/posts/999999", "", tokens["alice"])
	if code != 404 || env.Code != CodePostNotFound {
		t.Fatalf("missing delete: http=%d env=%+v, want 404/40004", code, env)
	}
	// Error envelope carries msg and no data field semantics (code != 0).
	if env.Msg == "" {
		t.Fatal("error envelope must carry msg")
	}
}

func TestHTTPEnvelopeShape(t *testing.T) {
	r, _, tokens, _ := newProfileHTTPSetup(t)
	_, env := doProfileReq(t, r, "GET", "/api/v1/profile/alice", "", "")
	if env.Code != 0 || env.Msg != "success" || len(env.Data) == 0 {
		t.Fatalf("success envelope wrong: %+v", env)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatalf("public data not object: %q", string(env.Data))
	}
	if _, ok := raw["user"]; !ok {
		t.Fatalf("user key missing in %q", string(env.Data))
	}
	if _, ok := raw["posts"]; !ok {
		t.Fatalf("posts key missing in %q", string(env.Data))
	}
	_, env = doProfileReq(t, r, "POST", "/api/v1/profile/posts", `{"media_type":"text","content":"hi"}`, tokens["alice"])
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatalf("create data not object: %q", string(env.Data))
	}
	if _, ok := raw["post"]; !ok {
		t.Fatalf("post key missing in %q", string(env.Data))
	}
}

func itoa(n uint) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
