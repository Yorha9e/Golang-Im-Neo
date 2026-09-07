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

func TestAdminRoutesNonAdminForbidden(t *testing.T) {
	db := newTestDB(t)
	h := newTestHub()
	svc := NewAdminService(db, h, zap.NewNop())
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
}

func TestAdminRoutesUnauthenticated(t *testing.T) {
	db := newTestDB(t)
	svc := NewAdminService(db, newTestHub(), zap.NewNop())
	r := newTestRouter(t, db, svc)
	code, env := doPOST(t, r, "/api/v1/admin/broadcast", `{"content":"hi"}`, "")
	if code != 401 || env.Code != CodeUnauthorized {
		t.Fatalf("broadcast without token: http=%d env=%+v, want 401/10002", code, env)
	}
}

func TestAdminBanKickBroadcastFlow(t *testing.T) {
	db := newTestDB(t)
	h := newTestHub()
	svc := NewAdminService(db, h, zap.NewNop())
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
