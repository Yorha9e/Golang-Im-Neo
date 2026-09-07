// Profile end-to-end (M2 integration proof): public profile card, authed
// profile update, per-media-type post creation with the link-preview
// reservation, the public post jump-card, and owner/negative authz — all
// against the fully-wired app (app.Build + httptest server) with a
// self-contained harness.
package test

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"golang-im-neo-system/internal/app"
	"golang-im-neo-system/internal/config"
)

const pePass = "password123"

func peConfig(t *testing.T, dbPath string) *config.Config {
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
			JWTSecret:        "pe-" + hex.EncodeToString(rb[:]),
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

type peEnv struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

var peHTTP = &http.Client{Timeout: 10 * time.Second}

// peDo issues one JSON request and returns the raw HTTP status + envelope
// without failing on business-error codes (for negative assertions).
func peDo(t *testing.T, method, base, path string, body interface{}, token string) (int, peEnv) {
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
	resp, err := peHTTP.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var env peEnv
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("%s %s: bad envelope %q: %v", method, path, peShort(raw), err)
	}
	return resp.StatusCode, env
}

func peShort(b []byte) string {
	if len(b) > 160 {
		return string(b[:160]) + "..."
	}
	return string(b)
}

func peMustOK(t *testing.T, method, path string, code int, env peEnv) json.RawMessage {
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

// peWantErr asserts an exact HTTP status + SSOT envelope code pair.
func peWantErr(t *testing.T, method, base, path string, body interface{}, token string, wantHTTP, wantCode int) {
	t.Helper()
	code, env := peDo(t, method, base, path, body, token)
	if code != wantHTTP || env.Code != wantCode {
		t.Fatalf("%s %s: http=%d code=%d msg=%q, want http=%d code=%d",
			method, path, code, env.Code, env.Msg, wantHTTP, wantCode)
	}
}

func pePOST(t *testing.T, base, path string, body interface{}, token string) json.RawMessage {
	t.Helper()
	code, env := peDo(t, http.MethodPost, base, path, body, token)
	return peMustOK(t, http.MethodPost, path, code, env)
}

func peGET(t *testing.T, base, path, token string) json.RawMessage {
	t.Helper()
	code, env := peDo(t, http.MethodGet, base, path, nil, token)
	return peMustOK(t, http.MethodGet, path, code, env)
}

func pePUT(t *testing.T, base, path string, body interface{}, token string) json.RawMessage {
	t.Helper()
	code, env := peDo(t, http.MethodPut, base, path, body, token)
	return peMustOK(t, http.MethodPut, path, code, env)
}

func peDELETE(t *testing.T, base, path, token string) json.RawMessage {
	t.Helper()
	code, env := peDo(t, http.MethodDelete, base, path, nil, token)
	return peMustOK(t, http.MethodDelete, path, code, env)
}

type peIdent struct {
	userID, at string
}

func peRegisterLogin(t *testing.T, base, user string) peIdent {
	t.Helper()
	data := pePOST(t, base, "/api/v1/auth/register",
		map[string]string{"username": user, "password": pePass}, "")
	var reg struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(data, &reg); err != nil || reg.UserID == "" {
		t.Fatalf("register %s: bad data %s", user, peShort(data))
	}
	data = pePOST(t, base, "/api/v1/auth/login", map[string]string{
		"username": user, "password": pePass,
		"device_class": "interactive", "device_name": "pe",
	}, "")
	var login struct {
		AccessToken string `json:"access_token"`
		UserID      string `json:"user_id"`
	}
	if err := json.Unmarshal(data, &login); err != nil || login.AccessToken == "" {
		t.Fatalf("login %s: bad data %s", user, peShort(data))
	}
	if login.UserID != reg.UserID {
		t.Fatalf("login uid %q != registered %q", login.UserID, reg.UserID)
	}
	return peIdent{userID: reg.UserID, at: login.AccessToken}
}

// ---------- profile JSON shapes ----------

type peLinkPreview struct {
	Title     string `json:"title"`
	CoverURL  string `json:"cover_url"`
	TargetURL string `json:"target_url"`
	Site      string `json:"site"`
}

type peUserCard struct {
	Username  string `json:"username"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
	Signature string `json:"signature"`
}

type pePost struct {
	ID           uint           `json:"id"`
	MediaType    string         `json:"media_type"`
	Content      string         `json:"content"`
	MediaURL     string         `json:"media_url"`
	BilibiliBVID string         `json:"bilibili_bvid"`
	LinkPreview  *peLinkPreview `json:"link_preview"`
	CreatedAt    int64          `json:"created_at"`
}

type peProfileData struct {
	User  peUserCard `json:"user"`
	Posts []pePost   `json:"posts"`
}

func peGetProfile(t *testing.T, base, username, token string) peProfileData {
	t.Helper()
	data := peGET(t, base, "/api/v1/profile/"+username, token)
	var pp peProfileData
	if err := json.Unmarshal(data, &pp); err != nil {
		t.Fatalf("GET profile %s: bad data %s", username, peShort(data))
	}
	return pp
}

func peCreatePost(t *testing.T, base string, token string, body map[string]interface{}) pePost {
	t.Helper()
	data := pePOST(t, base, "/api/v1/profile/posts", body, token)
	var out struct {
		Post pePost `json:"post"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.Post.ID == 0 {
		t.Fatalf("create post: bad data %s", peShort(data))
	}
	return out.Post
}

// ---------- the profile DoD test ----------

func TestProfileEndToEnd(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "e2e-pe.db")
	a, err := app.Build(peConfig(t, dbPath))
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

	// 1. Register + login users A and B.
	alice := peRegisterLogin(t, base, "pe_alice")
	bob := peRegisterLogin(t, base, "pe_bob")

	// 2. GET public profile of A: card fields, empty posts.
	pp := peGetProfile(t, base, "pe_alice", "")
	if pp.User.Username != "pe_alice" {
		t.Fatalf("public profile username = %q, want %q", pp.User.Username, "pe_alice")
	}
	if len(pp.Posts) != 0 {
		t.Fatalf("fresh profile posts = %d, want 0", len(pp.Posts))
	}

	// 3. PUT (tokenA) nickname+signature+avatar_url; GET public shows them.
	data := pePUT(t, base, "/api/v1/profile", map[string]string{
		"nickname": "Alice", "signature": "hello world", "avatar_url": "https://x/a.png",
	}, alice.at)
	var updated struct {
		User peUserCard `json:"user"`
	}
	if err := json.Unmarshal(data, &updated); err != nil {
		t.Fatalf("PUT profile: bad data %s", peShort(data))
	}
	if updated.User.Nickname != "Alice" || updated.User.Signature != "hello world" ||
		updated.User.AvatarURL != "https://x/a.png" {
		t.Fatalf("PUT profile not echoed: %+v", updated.User)
	}
	pp = peGetProfile(t, base, "pe_alice", "")
	if pp.User.Nickname != "Alice" || pp.User.Signature != "hello world" ||
		pp.User.AvatarURL != "https://x/a.png" {
		t.Fatalf("GET profile after PUT not persisted: %+v", pp.User)
	}

	// 4. PUT without token → 401; PUT with >255-rune signature → 400/10001.
	peWantErr(t, http.MethodPut, base, "/api/v1/profile",
		map[string]string{"nickname": "x"}, "", 401, 10002)
	peWantErr(t, http.MethodPut, base, "/api/v1/profile",
		map[string]string{"signature": strings.Repeat("s", 256)}, alice.at, 400, 10001)

	// 5. Four posts: text, image, video, bilibili (with link preview).
	p1 := peCreatePost(t, base, alice.at,
		map[string]interface{}{"media_type": "text", "content": "first post"})
	p2 := peCreatePost(t, base, alice.at,
		map[string]interface{}{"media_type": "image", "content": "img", "media_url": "https://x/i.jpg"})
	if p2.MediaURL != "https://x/i.jpg" {
		t.Fatalf("image post media_url = %q", p2.MediaURL)
	}
	p3 := peCreatePost(t, base, alice.at,
		map[string]interface{}{"media_type": "video", "content": "vid", "media_url": "https://x/v.mp4"})
	p4 := peCreatePost(t, base, alice.at, map[string]interface{}{
		"media_type": "bilibili", "content": "vid", "bilibili_bvid": "BV1xx411c7mD",
		"link_preview": map[string]string{
			"title": "T", "cover_url": "https://x/c.jpg",
			"target_url": "https://www.bilibili.com/video/BV1xx411c7mD", "site": "bilibili",
		},
	})
	if p4.LinkPreview == nil || p4.LinkPreview.Site != "bilibili" {
		t.Fatalf("bilibili post link_preview did not round-trip: %+v", p4.LinkPreview)
	}
	_ = p3

	// 6. GET public profile: 4 posts, newest-first, text content present.
	pp = peGetProfile(t, base, "pe_alice", "")
	if len(pp.Posts) != 4 {
		t.Fatalf("profile posts = %d, want 4", len(pp.Posts))
	}
	if pp.Posts[0].ID != p4.ID {
		t.Fatalf("newest post id = %d, want P4 %d", pp.Posts[0].ID, p4.ID)
	}
	foundText := false
	for _, p := range pp.Posts {
		if p.ID == p1.ID && p.Content == "first post" {
			foundText = true
		}
	}
	if !foundText {
		t.Fatalf("text post content missing in profile list (%s)", peShort(mustMarshal(t, pp.Posts)))
	}

	// 7. GET public jump card for P4.
	data = peGET(t, base, "/api/v1/posts/"+itoa(p4.ID)+"/card", "")
	var card struct {
		PostID      uint           `json:"post_id"`
		MediaType   string         `json:"media_type"`
		LinkPreview *peLinkPreview `json:"link_preview"`
	}
	if err := json.Unmarshal(data, &card); err != nil {
		t.Fatalf("GET card: bad data %s", peShort(data))
	}
	if card.LinkPreview == nil || card.LinkPreview.Site != "bilibili" ||
		card.LinkPreview.TargetURL != "https://www.bilibili.com/video/BV1xx411c7mD" {
		t.Fatalf("card link_preview wrong: %+v", card.LinkPreview)
	}

	// 8. Negatives: bad media_type → 400/10001; bad bvid → 400/40003;
	// empty text content → 400/10001; POST without token → 401.
	peWantErr(t, http.MethodPost, base, "/api/v1/profile/posts",
		map[string]interface{}{"media_type": "nope", "content": "x"}, alice.at, 400, 10001)
	peWantErr(t, http.MethodPost, base, "/api/v1/profile/posts",
		map[string]interface{}{"media_type": "bilibili", "content": "x", "bilibili_bvid": "BAD"}, alice.at, 400, 40003)
	peWantErr(t, http.MethodPost, base, "/api/v1/profile/posts",
		map[string]interface{}{"media_type": "text", "content": ""}, alice.at, 400, 10001)
	peWantErr(t, http.MethodPost, base, "/api/v1/profile/posts",
		map[string]interface{}{"media_type": "text", "content": "x"}, "", 401, 10002)

	// 9. DELETE P1 with tokenB (non-owner) → 403/10003; DELETE P999999 → 404/40004.
	peWantErr(t, http.MethodDelete, base, "/api/v1/profile/posts/"+itoa(p1.ID), nil, bob.at, 403, 10003)
	peWantErr(t, http.MethodDelete, base, "/api/v1/profile/posts/999999", nil, alice.at, 404, 40004)

	// 10. DELETE P1 with tokenA → 200; card P1 → 404/40004; profile → 3 posts.
	peDELETE(t, base, "/api/v1/profile/posts/"+itoa(p1.ID), alice.at)
	peWantErr(t, http.MethodGet, base, "/api/v1/posts/"+itoa(p1.ID)+"/card", nil, "", 404, 40004)
	pp = peGetProfile(t, base, "pe_alice", "")
	if len(pp.Posts) != 3 {
		t.Fatalf("profile posts after delete = %d, want 3", len(pp.Posts))
	}
	for _, p := range pp.Posts {
		if p.ID == p1.ID {
			t.Fatalf("deleted post %d still listed", p1.ID)
		}
	}

	// 11. GET unknown profile → 404/20001.
	peWantErr(t, http.MethodGet, base, "/api/v1/profile/pe_no_such_user", nil, "", 404, 20001)
}

func itoa(n uint) string {
	return strconv.FormatUint(uint64(n), 10)
}

func mustMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
