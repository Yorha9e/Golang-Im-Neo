package group

import (
	"encoding/json"
	"fmt"
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

const groupTestSecret = "group-test-secret-32chars-long!!"

func newGroupHTTPSetup(t *testing.T) (*gin.Engine, *gorm.DB, *GroupService, map[string]string, map[string]string) {
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
		{"u_carol", "carol"},
	} {
		if err := db.Create(&store.User{
			ID: u.id, Username: u.name, PasswordHash: "hash",
			Role: "user", Status: 1, TokenVersion: 1,
		}).Error; err != nil {
			t.Fatalf("seed %q: %v", u.name, err)
		}
	}
	svc := NewGroupService(db, nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	rg := r.Group("/api/v1/groups")
	RegisterRoutes(rg, svc, middleware.JWTAuthMiddleware(groupTestSecret, db))

	tokens := map[string]string{}
	ids := map[string]string{"alice": "u_alice", "bob": "u_bob", "carol": "u_carol"}
	for name, uid := range ids {
		tokens[name] = signGroupToken(t, groupTestSecret, uid, name, 1)
	}
	return r, db, svc, tokens, ids
}

func signGroupToken(t *testing.T, secret, userID, username string, tv int) string {
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

type groupEnv struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func doGroupReq(t *testing.T, r http.Handler, method, path, body, token string) (int, groupEnv) {
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
	var env groupEnv
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s %s: bad envelope %q: %v", method, path, w.Body.String(), err)
	}
	return w.Code, env
}

func mustCreateGroupHTTP(t *testing.T, r http.Handler, token, name string) GroupView {
	t.Helper()
	code, env := doGroupReq(t, r, "POST", "/api/v1/groups", `{"name":"`+name+`"}`, token)
	if code != 201 || env.Code != 0 {
		t.Fatalf("create %q: http=%d env=%+v", name, code, env)
	}
	var v GroupView
	if err := json.Unmarshal(env.Data, &v); err != nil || v.GroupID == "" {
		t.Fatalf("create data: %q err=%v", string(env.Data), err)
	}
	return v
}

func seedHistory(t *testing.T, db *gorm.DB, groupID string, n int, from string) {
	t.Helper()
	now := time.Now()
	for i := 1; i <= n; i++ {
		m := store.Message{
			CovID:       BuildCovID(groupID),
			Seq:         int64(i),
			StanzaID:    fmt.Sprintf("stanza-%d", i),
			ChatType:    "groupchat",
			FromUID:     from,
			ToUID:       BuildCovID(groupID),
			ContentType: 1,
			Content:     fmt.Sprintf("msg-%d", i),
			Timestamp:   now.Add(time.Duration(i) * time.Second).UnixMilli(),
			CreatedAt:   now,
		}
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("seed msg %d: %v", i, err)
		}
	}
}

func TestGroupHTTPGuardEnforced(t *testing.T) {
	r, _, _, _, _ := newGroupHTTPSetup(t)
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/v1/groups", `{"name":"g"}`},
		{"POST", "/api/v1/groups/", `{"name":"g"}`},
		{"GET", "/api/v1/groups", ""},
		{"GET", "/api/v1/groups/", ""},
		{"GET", "/api/v1/groups/g1", ""},
		{"PATCH", "/api/v1/groups/g1", `{"name":"x"}`},
		{"POST", "/api/v1/groups/g1/invite", `{"user_ids":["u_bob"]}`},
		{"POST", "/api/v1/groups/g1/join", ""},
		{"POST", "/api/v1/groups/g1/leave", ""},
		{"POST", "/api/v1/groups/g1/dismiss", ""},
		{"DELETE", "/api/v1/groups/g1/members/u_bob", ""},
		{"PATCH", "/api/v1/groups/g1/members/u_bob/role", `{"role":"admin"}`},
		{"PATCH", "/api/v1/groups/g1/members/u_bob/mute", `{"muted":true}`},
		{"GET", "/api/v1/groups/g1/members", ""},
		{"GET", "/api/v1/groups/g1/history", ""},
	} {
		code, env := doGroupReq(t, r, tc.method, tc.path, tc.body, "")
		if code != 401 || env.Code != CodeUnauthorized {
			t.Fatalf("%s %s no-auth: http=%d env=%+v, want 401/10002", tc.method, tc.path, code, env)
		}
	}
	code, env := doGroupReq(t, r, "GET", "/api/v1/groups", "", "garbage")
	if code != 401 || env.Code != CodeUnauthorized {
		t.Fatalf("bad token: http=%d env=%+v, want 401/10002", code, env)
	}
}

func TestGroupHTTPFullFlow(t *testing.T) {
	r, db, _, tokens, ids := newGroupHTTPSetup(t)

	g := mustCreateGroupHTTP(t, r, tokens["alice"], "gophers")

	// List mine: alice sees 1, bob sees 0.
	code, env := doGroupReq(t, r, "GET", "/api/v1/groups", "", tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("list: http=%d env=%+v", code, env)
	}
	var mine []GroupView
	if err := json.Unmarshal(env.Data, &mine); err != nil || len(mine) != 1 || mine[0].GroupID != g.GroupID {
		t.Fatalf("list data: %q err=%v", string(env.Data), err)
	}
	code, env = doGroupReq(t, r, "GET", "/api/v1/groups/", "", tokens["bob"])
	var bobMine []GroupView
	_ = json.Unmarshal(env.Data, &bobMine)
	if code != 200 || len(bobMine) != 0 {
		t.Fatalf("bob list: http=%d env=%+v", code, env)
	}

	// Non-member GET -> 40002.
	code, env = doGroupReq(t, r, "GET", "/api/v1/groups/"+g.GroupID, "", tokens["bob"])
	if code == 200 || env.Code != CodeGroupNotMember {
		t.Fatalf("non-member get: http=%d env=%+v, want 40002 non-200", code, env)
	}

	// Invite bob + carol.
	code, env = doGroupReq(t, r, "POST", "/api/v1/groups/"+g.GroupID+"/invite",
		`{"user_ids":["`+ids["bob"]+`","`+ids["carol"]+`"]}`, tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("invite: http=%d env=%+v", code, env)
	}
	var inv struct {
		Invited []string `json:"invited"`
	}
	if err := json.Unmarshal(env.Data, &inv); err != nil || len(inv.Invited) != 2 {
		t.Fatalf("invite data: %q err=%v", string(env.Data), err)
	}

	// Bob reads group + members.
	code, env = doGroupReq(t, r, "GET", "/api/v1/groups/"+g.GroupID, "", tokens["bob"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("bob get: http=%d env=%+v", code, env)
	}
	var gv GroupView
	if err := json.Unmarshal(env.Data, &gv); err != nil || gv.MemberCount != 3 {
		t.Fatalf("get data: %q err=%v", string(env.Data), err)
	}
	code, env = doGroupReq(t, r, "GET", "/api/v1/groups/"+g.GroupID+"/members", "", tokens["bob"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("members: http=%d env=%+v", code, env)
	}
	var members []MemberView
	if err := json.Unmarshal(env.Data, &members); err != nil || len(members) != 3 {
		t.Fatalf("members data: %q err=%v", string(env.Data), err)
	}

	// Member PATCH -> 40003; owner PATCH renames.
	code, env = doGroupReq(t, r, "PATCH", "/api/v1/groups/"+g.GroupID, `{"name":"x"}`, tokens["bob"])
	if code == 200 || env.Code != CodeGroupPermission {
		t.Fatalf("member patch: http=%d env=%+v, want 40003", code, env)
	}
	code, env = doGroupReq(t, r, "PATCH", "/api/v1/groups/"+g.GroupID,
		`{"name":"g2","announcement":"hi"}`, tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("owner patch: http=%d env=%+v", code, env)
	}
	var patched GroupView
	if err := json.Unmarshal(env.Data, &patched); err != nil || patched.Name != "g2" || patched.Announcement != "hi" {
		t.Fatalf("patched data: %q err=%v", string(env.Data), err)
	}

	// Role + mute.
	code, env = doGroupReq(t, r, "PATCH", "/api/v1/groups/"+g.GroupID+"/members/"+ids["bob"]+"/role",
		`{"role":"admin"}`, tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("set role: http=%d env=%+v", code, env)
	}
	code, env = doGroupReq(t, r, "PATCH", "/api/v1/groups/"+g.GroupID+"/members/"+ids["carol"]+"/mute",
		`{"muted":true}`, tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("mute: http=%d env=%+v", code, env)
	}

	// History: seed + read.
	seedHistory(t, db, g.GroupID, 5, ids["alice"])
	code, env = doGroupReq(t, r, "GET", "/api/v1/groups/"+g.GroupID+"/history?limit=2", "", tokens["bob"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("history: http=%d env=%+v", code, env)
	}
	var hist struct {
		Messages []struct {
			Seq       int64  `json:"seq"`
			FromUID   string `json:"from_uid"`
			Content   string `json:"content"`
			Extra     string `json:"extra"`
			Timestamp int64  `json:"timestamp"`
			StanzaID  string `json:"stanza_id"`
		} `json:"messages"`
		HasMore bool `json:"has_more"`
	}
	if err := json.Unmarshal(env.Data, &hist); err != nil {
		t.Fatalf("history data: %q err=%v", string(env.Data), err)
	}
	if len(hist.Messages) != 2 || !hist.HasMore {
		t.Fatalf("history window: %+v, want 2 msgs + has_more", hist)
	}
	// Latest window, ascending inside.
	if hist.Messages[0].Seq != 4 || hist.Messages[1].Seq != 5 {
		t.Fatalf("history seqs = %d,%d, want 4,5", hist.Messages[0].Seq, hist.Messages[1].Seq)
	}

	// Bob leaves; carol kicked; alice dismisses.
	code, env = doGroupReq(t, r, "POST", "/api/v1/groups/"+g.GroupID+"/leave", "", tokens["bob"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("leave: http=%d env=%+v", code, env)
	}
	code, env = doGroupReq(t, r, "DELETE", "/api/v1/groups/"+g.GroupID+"/members/"+ids["carol"], "", tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("kick: http=%d env=%+v", code, env)
	}
	code, env = doGroupReq(t, r, "POST", "/api/v1/groups/"+g.GroupID+"/dismiss", "", tokens["bob"])
	if code == 200 || env.Code != CodeGroupPermission {
		t.Fatalf("non-owner dismiss: http=%d env=%+v, want 40003", code, env)
	}
	code, env = doGroupReq(t, r, "POST", "/api/v1/groups/"+g.GroupID+"/dismiss", "", tokens["alice"])
	if code != 200 || env.Code != 0 {
		t.Fatalf("dismiss: http=%d env=%+v", code, env)
	}
}

func TestGroupHTTPBusinessErrors(t *testing.T) {
	r, _, _, tokens, ids := newGroupHTTPSetup(t)
	g := mustCreateGroupHTTP(t, r, tokens["alice"], "g1")

	// Empty name -> 10001/400.
	code, env := doGroupReq(t, r, "POST", "/api/v1/groups", `{"name":""}`, tokens["alice"])
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("empty name: http=%d env=%+v, want 400/10001", code, env)
	}
	// Bad body -> 10001.
	code, env = doGroupReq(t, r, "POST", "/api/v1/groups", `not-json`, tokens["alice"])
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("bad body: http=%d env=%+v, want 400/10001", code, env)
	}
	// Non-member history -> 40002 non-200.
	code, env = doGroupReq(t, r, "GET", "/api/v1/groups/"+g.GroupID+"/history", "", tokens["bob"])
	if code == 200 || env.Code != CodeGroupNotMember {
		t.Fatalf("non-member history: http=%d env=%+v, want 40002", code, env)
	}
	// Non-member members list -> 40002.
	code, env = doGroupReq(t, r, "GET", "/api/v1/groups/"+g.GroupID+"/members", "", tokens["bob"])
	if code == 200 || env.Code != CodeGroupNotMember {
		t.Fatalf("non-member members: http=%d env=%+v, want 40002", code, env)
	}
	// Bad role -> 40006/400.
	code, env = doGroupReq(t, r, "PATCH", "/api/v1/groups/"+g.GroupID+"/members/"+ids["alice"]+"/role",
		`{"role":"boss"}`, tokens["alice"])
	if code != 400 || env.Code != CodeGroupRoleInvalid {
		t.Fatalf("bad role: http=%d env=%+v, want 400/40006", code, env)
	}
	// Missing muted -> 10001.
	code, env = doGroupReq(t, r, "PATCH", "/api/v1/groups/"+g.GroupID+"/members/"+ids["alice"]+"/mute",
		`{}`, tokens["alice"])
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("missing muted: http=%d env=%+v, want 400/10001", code, env)
	}
	// Invite unknown user -> 40009/404.
	code, env = doGroupReq(t, r, "POST", "/api/v1/groups/"+g.GroupID+"/invite",
		`{"user_ids":["u_ghost"]}`, tokens["alice"])
	if code != 404 || env.Code != CodeGroupUserMissing {
		t.Fatalf("invite ghost: http=%d env=%+v, want 404/40009", code, env)
	}
	// Join missing group -> 40001/404.
	code, env = doGroupReq(t, r, "POST", "/api/v1/groups/nope/join", "", tokens["alice"])
	if code != 404 || env.Code != CodeGroupNotFound {
		t.Fatalf("join missing: http=%d env=%+v, want 404/40001", code, env)
	}
	// Error envelope carries msg and no 200.
	if env.Msg == "" {
		t.Fatal("error envelope must carry msg")
	}
	// Success envelope shape.
	_, env = doGroupReq(t, r, "GET", "/api/v1/groups", "", tokens["alice"])
	if env.Code != 0 || env.Msg != "success" || len(env.Data) == 0 {
		t.Fatalf("success envelope wrong: %+v", env)
	}
}

func TestGroupHTTPHistoryPagination(t *testing.T) {
	r, db, _, tokens, ids := newGroupHTTPSetup(t)
	g := mustCreateGroupHTTP(t, r, tokens["alice"], "g1")
	seedHistory(t, db, g.GroupID, 5, ids["alice"])

	getHist := func(t *testing.T, token, query string) (int, []map[string]interface{}, bool) {
		t.Helper()
		code, env := doGroupReq(t, r, "GET", "/api/v1/groups/"+g.GroupID+"/history"+query, "", token)
		if code != 200 || env.Code != 0 {
			t.Fatalf("history%s: http=%d env=%+v", query, code, env)
		}
		var body struct {
			Messages []map[string]interface{} `json:"messages"`
			HasMore  bool                     `json:"has_more"`
		}
		if err := json.Unmarshal(env.Data, &body); err != nil {
			t.Fatalf("history%s data: %q err=%v", query, string(env.Data), err)
		}
		if body.Messages == nil {
			t.Fatalf("history%s: messages must be [] not null", query)
		}
		return code, body.Messages, body.HasMore
	}

	// Default window: all 5 ascending, no more.
	_, msgs, more := getHist(t, tokens["alice"], "")
	if len(msgs) != 5 || more {
		t.Fatalf("default: n=%d more=%v, want 5/false", len(msgs), more)
	}
	for i, m := range msgs {
		if int(m["seq"].(float64)) != i+1 {
			t.Fatalf("default order wrong at %d: %v", i, m)
		}
		if m["from_uid"] != ids["alice"] || m["content"] == "" || m["stanza_id"] == "" {
			t.Fatalf("message fields missing: %v", m)
		}
	}
	// before_seq window.
	_, msgs, more = getHist(t, tokens["alice"], "?before_seq=4&limit=2")
	if len(msgs) != 2 || !more {
		t.Fatalf("before_seq: n=%d more=%v, want 2/true", len(msgs), more)
	}
	if int(msgs[0]["seq"].(float64)) != 2 || int(msgs[1]["seq"].(float64)) != 3 {
		t.Fatalf("before_seq seqs = %v,%v, want 2,3", msgs[0]["seq"], msgs[1]["seq"])
	}
	// Tail window: no more.
	_, msgs, more = getHist(t, tokens["alice"], "?before_seq=3&limit=10")
	if len(msgs) != 2 || more {
		t.Fatalf("tail: n=%d more=%v, want 2/false", len(msgs), more)
	}
	// Limit clamps to [1,100]: oversized limit returns everything.
	_, msgs, more = getHist(t, tokens["alice"], "?limit=500")
	if len(msgs) != 5 || more {
		t.Fatalf("clamped: n=%d more=%v, want 5/false", len(msgs), more)
	}
}
