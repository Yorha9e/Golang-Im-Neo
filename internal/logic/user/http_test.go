package user

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"golang-im-neo-system/internal/middleware"
	"golang-im-neo-system/internal/store"
	"gorm.io/gorm"
)

const userTestSecret = "user-test-secret-32chars-long!!!"

type userEnvBody struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func newUserTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := store.NewDB(store.DBConfig{Path: filepath.Join(t.TempDir(), "u.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}

func newUserTestRouter(t *testing.T, db *gorm.DB, svc *UserService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	rg := r.Group("/api/v1/users")
	RegisterRoutes(rg, svc, middleware.JWTAuthMiddleware(userTestSecret, db))
	return r
}

func userSignAs(t *testing.T, u *store.User, sessionID string) string {
	t.Helper()
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": u.ID, "username": u.Username, "role": u.Role,
		"session_id": sessionID, "device_class": "interactive", "tv": u.TokenVersion,
		"iat": now.Unix(), "exp": now.Add(2 * time.Hour).Unix(),
	})
	s, err := tok.SignedString([]byte(userTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func userDoGET(t *testing.T, r http.Handler, path, token string) (int, userEnvBody) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var env userEnvBody
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("GET %s: bad envelope: %v (%s)", path, err, w.Body.String())
	}
	return w.Code, env
}

func seedUserRow(t *testing.T, db *gorm.DB, id, username, nickname string, status int8, createdAt time.Time) *store.User {
	t.Helper()
	u := &store.User{
		ID: id, Username: username, Nickname: nickname,
		PasswordHash: "hash", Role: "user", Status: status,
		TokenVersion: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	return u
}

type userListData struct {
	Total int64      `json:"total"`
	Page  int        `json:"page"`
	Limit int        `json:"limit"`
	Users []UserItem `json:"users"`
}

func TestUserListPagination(t *testing.T) {
	db := newUserTestDB(t)
	svc := NewUserService(db, zap.NewNop())
	r := newUserTestRouter(t, db, svc)

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		seedUserRow(t, db, "u_pag_"+string(rune('0'+i)), "paguser"+string(rune('0'+i)), "nick"+string(rune('0'+i)), 1, ts)
	}
	me := seedUserRow(t, db, "u_me", "meuser", "me", 1, base.Add(10*time.Minute))
	tok := userSignAs(t, me, "s-me")

	code, env := userDoGET(t, r, "/api/v1/users?page=1&limit=2", tok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("page1: http=%d env=%+v", code, env)
	}
	var d userListData
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// 5 pagusers + me = 6 active users.
	if d.Total != 6 {
		t.Fatalf("total = %d, want 6", d.Total)
	}
	if d.Page != 1 || d.Limit != 2 {
		t.Fatalf("page/limit = %d/%d, want 1/2", d.Page, d.Limit)
	}
	if len(d.Users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(d.Users))
	}

	code, env = userDoGET(t, r, "/api/v1/users?page=3&limit=2", tok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("page3: http=%d env=%+v", code, env)
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(d.Users) != 2 {
		t.Fatalf("page3 len = %d, want 2", len(d.Users))
	}

	// Default page/limit when omitted.
	code, env = userDoGET(t, r, "/api/v1/users", tok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("default: http=%d env=%+v", code, env)
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Page != 1 || d.Limit != 20 {
		t.Fatalf("default page/limit = %d/%d, want 1/20", d.Page, d.Limit)
	}
	if d.Total != 6 || len(d.Users) != 6 {
		t.Fatalf("default total=%d len=%d, want 6/6", d.Total, len(d.Users))
	}
}

func TestUserListKeywordSearch(t *testing.T) {
	db := newUserTestDB(t)
	svc := NewUserService(db, zap.NewNop())
	r := newUserTestRouter(t, db, svc)

	now := time.Now()
	seedUserRow(t, db, "u_k1", "alice123", "Alice", 1, now)
	seedUserRow(t, db, "u_k2", "bob456", "Bobby", 1, now.Add(time.Second))
	seedUserRow(t, db, "u_k3", "carol789", "Carol", 1, now.Add(2*time.Second))
	me := seedUserRow(t, db, "u_kme", "kem", "kem", 1, now.Add(3*time.Second))
	tok := userSignAs(t, me, "s-me")

	code, env := userDoGET(t, r, "/api/v1/users?keyword=alice", tok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("keyword: http=%d env=%+v", code, env)
	}
	var d userListData
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Total != 1 || len(d.Users) != 1 || d.Users[0].Username != "alice123" {
		t.Fatalf("keyword alice: %+v", d)
	}

	// Nickname match.
	code, env = userDoGET(t, r, "/api/v1/users?keyword=Bobby", tok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("nickname: http=%d env=%+v", code, env)
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Total != 1 || d.Users[0].Username != "bob456" {
		t.Fatalf("keyword Bobby: %+v", d)
	}

	// No match.
	code, env = userDoGET(t, r, "/api/v1/users?keyword=zzz_nomatch", tok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("nomatch: http=%d env=%+v", code, env)
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Total != 0 || len(d.Users) != 0 {
		t.Fatalf("nomatch total=%d len=%d, want 0/0", d.Total, len(d.Users))
	}
	if d.Users == nil {
		t.Fatalf("users should be non-nil empty slice")
	}
}

func TestUserListExcludesBanned(t *testing.T) {
	db := newUserTestDB(t)
	svc := NewUserService(db, zap.NewNop())
	r := newUserTestRouter(t, db, svc)

	now := time.Now()
	seedUserRow(t, db, "u_ok", "gooduser", "good", 1, now)
	seedUserRow(t, db, "u_banned", "baduser", "bad", 2, now.Add(time.Second))
	// Status 0 cannot be stored via Create (GORM omits zero value and the DB
	// default 1 applies), so force it with an explicit UPDATE to cover the
	// status != 1 exclusion path.
	seedUserRow(t, db, "u_zero", "zerouser_tmp", "zero", 1, now.Add(2*time.Second))
	if err := db.Model(&store.User{}).Where("id = ?", "u_zero").Updates(map[string]interface{}{"username": "zerouser", "status": 0}).Error; err != nil {
		t.Fatalf("force status 0: %v", err)
	}
	me := seedUserRow(t, db, "u_me2", "lister", "lister", 1, now.Add(3*time.Second))
	tok := userSignAs(t, me, "s-me")

	code, env := userDoGET(t, r, "/api/v1/users?keyword=user", tok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("list: http=%d env=%+v", code, env)
	}
	var d userListData
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, u := range d.Users {
		if u.Username == "baduser" || u.Username == "zerouser" {
			t.Fatalf("banned/inactive user %q should be excluded: %+v", u.Username, d.Users)
		}
	}
	if d.Total != 1 {
		t.Fatalf("total = %d, want 1 (only gooduser matches 'user'; lister has no 'user')", d.Total)
	}

	// Service-level: banned users never appear even without keyword.
	res, err := svc.ListUsers(1, 20, "")
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	for _, u := range res.Users {
		if u.UserID == "u_banned" || u.UserID == "u_zero" {
			t.Fatalf("banned user in list: %+v", u)
		}
	}
	if res.Total != 2 {
		t.Fatalf("total without keyword = %d, want 2 (gooduser + lister)", res.Total)
	}
	// Safe fields: no password hash leaked via JSON.
	raw, _ := json.Marshal(res.Users)
	var arr []map[string]interface{}
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatal(err)
	}
	for _, m := range arr {
		if _, ok := m["password_hash"]; ok {
			t.Fatalf("password_hash leaked: %v", m)
		}
	}
}

func TestUserListUnauthenticated(t *testing.T) {
	db := newUserTestDB(t)
	svc := NewUserService(db, zap.NewNop())
	r := newUserTestRouter(t, db, svc)

	code, env := userDoGET(t, r, "/api/v1/users", "")
	if code != 401 || env.Code != CodeUnauthorized {
		t.Fatalf("no token: http=%d env=%+v, want 401/10002", code, env)
	}
}

func TestUserListLimitClamp(t *testing.T) {
	db := newUserTestDB(t)
	svc := NewUserService(db, zap.NewNop())

	res, err := svc.ListUsers(0, 0, "")
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if res.Page != 1 || res.Limit != 20 {
		t.Fatalf("defaults page/limit = %d/%d, want 1/20", res.Page, res.Limit)
	}
	res, err = svc.ListUsers(-1, 500, "")
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if res.Page != 1 || res.Limit != 50 {
		t.Fatalf("clamped page/limit = %d/%d, want 1/50", res.Page, res.Limit)
	}
}
