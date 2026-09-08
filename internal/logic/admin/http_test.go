package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"golang-im-neo-system/internal/gateway"
	"golang-im-neo-system/internal/middleware"
	"golang-im-neo-system/internal/store"
	"gorm.io/gorm"
)

const testSecret = "test-secret-32chars-long-for-tests!"

type envBody struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func newTestRouter(t *testing.T, db *gorm.DB, svc *AdminService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	rg := r.Group("/api/v1/admin")
	RegisterRoutes(rg, svc,
		middleware.JWTAuthMiddleware(testSecret, db),
		middleware.AdminAuthMiddleware(),
	)
	return r
}
func signAs(t *testing.T, u *store.User, sessionID string) string {
	t.Helper()
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": u.ID, "username": u.Username, "role": u.Role,
		"session_id": sessionID, "device_class": "interactive", "tv": u.TokenVersion,
		"iat": now.Unix(), "exp": now.Add(2 * time.Hour).Unix(),
	})
	s, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func doPOST(t *testing.T, r http.Handler, path, body, token string) (int, envBody) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var env envBody
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("POST %s: bad envelope: %v (%s)", path, err, w.Body.String())
	}
	return w.Code, env
}

func doGET(t *testing.T, r http.Handler, path, token string) (int, envBody) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var env envBody
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("GET %s: bad envelope: %v (%s)", path, err, w.Body.String())
	}
	return w.Code, env
}

type stubBatch struct{ n int }

func (s *stubBatch) QueueLen() int { return s.n }

func TestAdminRoutesNonAdminForbidden(t *testing.T) {
	db := newTestDB(t)
	h := newTestHub()
	svc := NewAdminService(db, h, nil, zap.NewNop())
	r := newTestRouter(t, db, svc)

	user := seedUser(t, db, "u_plain")
	tok := signAs(t, user, "s-x")

	for _, path := range []string{
		"/api/v1/admin/users/u_victim/ban",
		"/api/v1/admin/sessions/s-desk/kick",
	} {
		code, env := doPOST(t, r, path, `{}`, tok)
		if code != 403 || env.Code != CodeForbidden {
			t.Fatalf("POST %s as user: http=%d env=%+v, want 403/10003", path, code, env)
		}
	}
	code, env := doPOST(t, r, "/api/v1/admin/broadcast", `{"content":"hi"}`, tok)
	if code != 403 || env.Code != CodeForbidden {
		t.Fatalf("broadcast as user: http=%d env=%+v, want 403/10003", code, env)
	}
	code, env = doGET(t, r, "/api/v1/admin/stats", tok)
	if code != 403 || env.Code != CodeForbidden {
		t.Fatalf("stats as user: http=%d env=%+v, want 403/10003", code, env)
	}
}

func TestAdminRoutesUnauthenticated(t *testing.T) {
	db := newTestDB(t)
	svc := NewAdminService(db, newTestHub(), nil, zap.NewNop())
	r := newTestRouter(t, db, svc)
	code, env := doPOST(t, r, "/api/v1/admin/broadcast", `{"content":"hi"}`, "")
	if code != 401 || env.Code != CodeUnauthorized {
		t.Fatalf("broadcast without token: http=%d env=%+v, want 401/10002", code, env)
	}
	code, env = doGET(t, r, "/api/v1/admin/stats", "")
	if code != 401 || env.Code != CodeUnauthorized {
		t.Fatalf("stats without token: http=%d env=%+v, want 401/10002", code, env)
	}
}

func TestAdminBanKickBroadcastFlow(t *testing.T) {
	db := newTestDB(t)
	h := newTestHub()
	svc := NewAdminService(db, h, nil, zap.NewNop())
	r := newTestRouter(t, db, svc)

	// Admin identity.
	admin := &store.User{ID: "u_admin", Username: "root", PasswordHash: "hash", Role: "admin", Status: 1, TokenVersion: 1}
	if err := db.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	adminTok := signAs(t, admin, "s-admin")

	// Victim with two live hub connections.
	victim := seedUser(t, db, "u_victim2")
	seedSession(t, db, "s-desk2", victim.ID, "interactive")
	seedSession(t, db, "s-hw2", victim.ID, "hardware")
	desk := gateway.NewClient(h, nil, victim.ID, "interactive", "s-desk2", zap.NewNop())
	hw := gateway.NewClient(h, nil, victim.ID, "hardware", "s-hw2", zap.NewNop())
	h.Register(desk)
	h.Register(hw)
	waitForCount(t, h, 2)

	// Precise kick via HTTP: only the desktop session drops.
	code, env := doPOST(t, r, "/api/v1/admin/sessions/s-desk2/kick", `{}`, adminTok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("kick: http=%d env=%+v", code, env)
	}
	waitForCount(t, h, 1)
	var sess store.UserSession
	if err := db.Where("id = ?", "s-desk2").First(&sess).Error; err != nil || sess.IsRevoked != 1 {
		t.Fatalf("s-desk2 revoked: %+v err=%v", sess, err)
	}

	// Kicking a missing session → 404/20001.
	code, env = doPOST(t, r, "/api/v1/admin/sessions/s_missing/kick", `{}`, adminTok)
	if code != 404 || env.Code != CodeUserNotFound {
		t.Fatalf("kick missing: http=%d env=%+v, want 404/20001", code, env)
	}

	// Broadcast validation + alias body.
	code, env = doPOST(t, r, "/api/v1/admin/broadcast", `{}`, adminTok)
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("broadcast empty: http=%d env=%+v, want 400/10001", code, env)
	}
	code, env = doPOST(t, r, "/api/v1/admin/broadcast", `{"message":"notice!"}`, adminTok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("broadcast alias: http=%d env=%+v", code, env)
	}
	got := recvTimeout(t, hw.Send)
	if len(got) == 0 {
		t.Fatalf("surviving session should receive broadcast")
	}

	// Ban via HTTP: remaining connection drops, user banned.
	code, env = doPOST(t, r, "/api/v1/admin/users/u_victim2/ban", `{}`, adminTok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("ban: http=%d env=%+v", code, env)
	}
	waitForCount(t, h, 0)
	var banned store.User
	if err := db.Where("id = ?", victim.ID).First(&banned).Error; err != nil || banned.Status != 2 {
		t.Fatalf("victim banned: %+v err=%v", banned, err)
	}

	// Ban missing user → 404/20001.
	code, env = doPOST(t, r, "/api/v1/admin/users/u_missing/ban", `{}`, adminTok)
	if code != 404 || env.Code != CodeUserNotFound {
		t.Fatalf("ban missing: http=%d env=%+v, want 404/20001", code, env)
	}
}

func TestAdminStats(t *testing.T) {
	db := newTestDB(t)
	h := newTestHub()
	batch := &stubBatch{n: 7}
	svc := NewAdminService(db, h, batch, zap.NewNop())
	r := newTestRouter(t, db, svc)

	admin := &store.User{ID: "u_admin", Username: "root", PasswordHash: "hash", Role: "admin", Status: 1, TokenVersion: 1}
	if err := db.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	adminTok := signAs(t, admin, "s-admin")

	// Seed counts: 1 admin + 2 users (one banned), 1 group, 2 messages.
	u1 := seedUser(t, db, "u_s1")
	u2 := seedUser(t, db, "u_s2")
	if err := db.Model(&store.User{}).Where("id = ?", u2.ID).Update("status", 2).Error; err != nil {
		t.Fatal(err)
	}
	_ = u1
	now := time.Now()
	if err := db.Create(&store.Group{ID: "g1", Name: "g", OwnerID: admin.ID, MaxMembers: 500, Status: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	for i, stanza := range []string{"st1", "st2"} {
		if err := db.Create(&store.Message{CovID: "cov:1", Seq: int64(i + 1), StanzaID: stanza, ChatType: "chat", FromUID: admin.ID, ToUID: u1.ID, ContentType: 1, Content: "hi", Status: 1, Timestamp: now.UnixMilli(), CreatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	c1 := gateway.NewClient(h, nil, u1.ID, "interactive", "s-1", zap.NewNop())
	h.Register(c1)
	waitForCount(t, h, 1)

	code, env := doGET(t, r, "/api/v1/admin/stats", adminTok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("stats as admin: http=%d env=%+v, want 200/0", code, env)
	}
	var data struct {
		Server struct {
			UptimeSeconds int64  `json:"uptime_seconds"`
			StartTime     string `json:"start_time"`
			GoVersion     string `json:"go_version"`
			NumGoroutine  int    `json:"num_goroutine"`
			Memory        struct {
				AllocBytes      uint64 `json:"alloc_bytes"`
				TotalAllocBytes uint64 `json:"total_alloc_bytes"`
				SysBytes        uint64 `json:"sys_bytes"`
				NumGC           uint32 `json:"num_gc"`
			} `json:"memory"`
		} `json:"server"`
		Gateway struct {
			OnlineConnections int `json:"online_connections"`
			ShardCount        int `json:"shard_count"`
		} `json:"gateway"`
		Store struct {
			BatchQueueLen int `json:"batch_queue_len"`
			BatchQueueCap int `json:"batch_queue_cap"`
			DBOpenConns   int `json:"db_open_conns"`
			DBInUse       int `json:"db_in_use"`
			DBIdle        int `json:"db_idle"`
		} `json:"store"`
		Counts struct {
			UsersTotal    int64 `json:"users_total"`
			UsersBanned   int64 `json:"users_banned"`
			GroupsTotal   int64 `json:"groups_total"`
			MessagesTotal int64 `json:"messages_total"`
		} `json:"counts"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("stats data unmarshal: %v (%s)", err, string(env.Data))
	}
	if data.Server.GoVersion == "" || data.Server.GoVersion[:2] != "go" {
		t.Fatalf("go_version = %q, want go*", data.Server.GoVersion)
	}
	if data.Server.NumGoroutine <= 0 {
		t.Fatalf("num_goroutine = %d, want >0", data.Server.NumGoroutine)
	}
	if data.Server.StartTime == "" {
		t.Fatalf("start_time empty")
	}
	if data.Server.UptimeSeconds < 0 {
		t.Fatalf("uptime_seconds = %d, want >=0", data.Server.UptimeSeconds)
	}
	if data.Server.Memory.SysBytes == 0 {
		t.Fatalf("memory sys_bytes = 0")
	}
	if data.Gateway.OnlineConnections != 1 {
		t.Fatalf("online_connections = %d, want 1", data.Gateway.OnlineConnections)
	}
	if data.Gateway.ShardCount != 32 {
		t.Fatalf("shard_count = %d, want 32", data.Gateway.ShardCount)
	}
	if data.Store.BatchQueueLen != 7 {
		t.Fatalf("batch_queue_len = %d, want 7", data.Store.BatchQueueLen)
	}
	if data.Store.BatchQueueCap != 10000 {
		t.Fatalf("batch_queue_cap = %d, want 10000", data.Store.BatchQueueCap)
	}
	if data.Counts.UsersTotal != 3 {
		t.Fatalf("users_total = %d, want 3", data.Counts.UsersTotal)
	}
	if data.Counts.UsersBanned != 1 {
		t.Fatalf("users_banned = %d, want 1", data.Counts.UsersBanned)
	}
	if data.Counts.GroupsTotal != 1 {
		t.Fatalf("groups_total = %d, want 1", data.Counts.GroupsTotal)
	}
	if data.Counts.MessagesTotal != 2 {
		t.Fatalf("messages_total = %d, want 2", data.Counts.MessagesTotal)
	}
}

func TestAdminStatsNilBatch(t *testing.T) {
	db := newTestDB(t)
	svc := NewAdminService(db, newTestHub(), nil, zap.NewNop())
	stats, err := svc.GetStats()
	if err != nil {
		t.Fatalf("GetStats nil batch: %v", err)
	}
	if stats.Store.BatchQueueLen != 0 || stats.Store.BatchQueueCap != 10000 {
		t.Fatalf("nil batch store = %+v", stats.Store)
	}
	if stats.Gateway.ShardCount != 32 {
		t.Fatalf("shard_count = %d", stats.Gateway.ShardCount)
	}
}

type adminUserListData struct {
	Total int64           `json:"total"`
	Page  int             `json:"page"`
	Limit int             `json:"limit"`
	Users []AdminUserItem `json:"users"`
}

func seedAdminUserRow(t *testing.T, db *gorm.DB, id, username, nickname string, status int8, createdAt time.Time) *store.User {
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

func newAdminToken(t *testing.T, db *gorm.DB) (string, *store.User) {
	t.Helper()
	admin := &store.User{ID: "u_admin_list", Username: "rootlist", PasswordHash: "hash", Role: "admin", Status: 1, TokenVersion: 1}
	if err := db.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	return signAs(t, admin, "s-admin"), admin
}

func TestAdminListUsersPagination(t *testing.T) {
	db := newTestDB(t)
	svc := NewAdminService(db, newTestHub(), nil, zap.NewNop())
	r := newTestRouter(t, db, svc)
	adminTok, _ := newAdminToken(t, db)

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		seedAdminUserRow(t, db, "u_al_"+string(rune('a'+i)), "adminpag"+string(rune('a'+i)), "nick"+string(rune('a'+i)), 1, ts)
	}

	code, env := doGET(t, r, "/api/v1/admin/users?page=1&limit=2", adminTok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("page1: http=%d env=%+v", code, env)
	}
	var d adminUserListData
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// 1 admin + 5 users = 6.
	if d.Total != 6 {
		t.Fatalf("total = %d, want 6", d.Total)
	}
	if d.Page != 1 || d.Limit != 2 || len(d.Users) != 2 {
		t.Fatalf("page/limit/len = %d/%d/%d, want 1/2/2", d.Page, d.Limit, len(d.Users))
	}

	code, env = doGET(t, r, "/api/v1/admin/users?page=3&limit=2", adminTok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("page3: http=%d env=%+v", code, env)
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(d.Users) != 2 {
		t.Fatalf("page3 len = %d, want 2", len(d.Users))
	}
}

func TestAdminListUsersStatusFilter(t *testing.T) {
	db := newTestDB(t)
	svc := NewAdminService(db, newTestHub(), nil, zap.NewNop())
	r := newTestRouter(t, db, svc)
	adminTok, _ := newAdminToken(t, db)

	now := time.Now()
	seedAdminUserRow(t, db, "u_sf_ok", "sfgood", "good", 1, now)
	seedAdminUserRow(t, db, "u_sf_ban", "sfbad", "bad", 2, now.Add(time.Second))

	code, env := doGET(t, r, "/api/v1/admin/users?status=2", adminTok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("status=2: http=%d env=%+v", code, env)
	}
	var d adminUserListData
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Total != 1 || len(d.Users) != 1 || d.Users[0].Username != "sfbad" {
		t.Fatalf("status=2: %+v", d)
	}
	if d.Users[0].Status != 2 {
		t.Fatalf("status field = %d, want 2", d.Users[0].Status)
	}

	code, env = doGET(t, r, "/api/v1/admin/users?status=1", adminTok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("status=1: http=%d env=%+v", code, env)
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, u := range d.Users {
		if u.Status != 1 {
			t.Fatalf("status=1 returned %+v", u)
		}
	}
	// Safe fields: no password hash.
	var arr []map[string]interface{}
	raw, _ := json.Marshal(d.Users)
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatal(err)
	}
	for _, m := range arr {
		if _, ok := m["password_hash"]; ok {
			t.Fatalf("password_hash leaked: %v", m)
		}
	}
}

func TestAdminListUsersKeyword(t *testing.T) {
	db := newTestDB(t)
	svc := NewAdminService(db, newTestHub(), nil, zap.NewNop())
	r := newTestRouter(t, db, svc)
	adminTok, _ := newAdminToken(t, db)

	now := time.Now()
	seedAdminUserRow(t, db, "u_kw1", "kwalice", "Alice", 1, now)
	seedAdminUserRow(t, db, "u_kw2", "kwbob", "Bobby", 2, now.Add(time.Second))

	// Keyword spans statuses (admin sees banned too).
	code, env := doGET(t, r, "/api/v1/admin/users?keyword=kw", adminTok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("keyword: http=%d env=%+v", code, env)
	}
	var d adminUserListData
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Total != 2 {
		t.Fatalf("keyword kw total = %d, want 2", d.Total)
	}

	code, env = doGET(t, r, "/api/v1/admin/users?keyword=Bobby", adminTok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("nickname: http=%d env=%+v", code, env)
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Total != 1 || d.Users[0].Username != "kwbob" {
		t.Fatalf("keyword Bobby: %+v", d)
	}

	// Combined status + keyword.
	code, env = doGET(t, r, "/api/v1/admin/users?status=1&keyword=kw", adminTok)
	if code != 200 || env.Code != 0 {
		t.Fatalf("combined: http=%d env=%+v", code, env)
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Total != 1 || d.Users[0].Username != "kwalice" {
		t.Fatalf("combined: %+v", d)
	}
}

func TestAdminListUsersForbidden(t *testing.T) {
	db := newTestDB(t)
	svc := NewAdminService(db, newTestHub(), nil, zap.NewNop())
	r := newTestRouter(t, db, svc)

	user := seedUser(t, db, "u_plain_list")
	tok := signAs(t, user, "s-x")

	code, env := doGET(t, r, "/api/v1/admin/users", tok)
	if code != 403 || env.Code != CodeForbidden {
		t.Fatalf("non-admin: http=%d env=%+v, want 403/10003", code, env)
	}
}

func TestAdminListUsersUnauthenticated(t *testing.T) {
	db := newTestDB(t)
	svc := NewAdminService(db, newTestHub(), nil, zap.NewNop())
	r := newTestRouter(t, db, svc)

	code, env := doGET(t, r, "/api/v1/admin/users", "")
	if code != 401 || env.Code != CodeUnauthorized {
		t.Fatalf("no token: http=%d env=%+v, want 401/10002", code, env)
	}
}
