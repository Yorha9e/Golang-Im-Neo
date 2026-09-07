package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/store"
	"golang-im-neo-system/pkg/util"
)

const testSecret = "test-secret-32chars-long-for-tests!"

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := store.NewDB(store.DBConfig{Path: filepath.Join(t.TempDir(), "t.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	// Release the SQLite file handle so t.TempDir() cleanup succeeds on Windows.
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}

func seedUser(t *testing.T, db *gorm.DB, username, role string, status int8, tv int) *store.User {
	t.Helper()
	hash, err := util.HashPassword("secure_password", bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	u := &store.User{
		ID: util.NewUserID(), Username: username, PasswordHash: hash,
		Role: role, Status: status, TokenVersion: tv,
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func signToken(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func validClaims(u *store.User) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"user_id": u.ID, "username": u.Username, "role": u.Role,
		"session_id": "s_test", "device_class": "interactive", "tv": u.TokenVersion,
		"iat": now.Unix(), "exp": now.Add(2 * time.Hour).Unix(),
	}
}

// serveThrough builds a gin engine with JWTAuthMiddleware and a probe handler
// that echoes the injected context keys.
func serveThrough(db *gorm.DB, secret, token string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(JWTAuthMiddleware(secret, db))
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"user_id": c.GetString("user_id"), "username": c.GetString("username"),
			"role": c.GetString("role"), "session_id": c.GetString("session_id"),
			"device_class": c.GetString("device_class"),
		})
	})
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

type errEnv struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func TestMissingHeader(t *testing.T) {
	db := newTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(JWTAuthMiddleware(testSecret, db))
	r.GET("/ping", func(c *gin.Context) { c.Status(200) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("missing header: http=%d, want 401", w.Code)
	}
	var env errEnv
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || env.Code != 10002 {
		t.Fatalf("missing header: %+v %v, want code 10002", env, err)
	}
}

func TestGarbageToken(t *testing.T) {
	db := newTestDB(t)
	w := serveThrough(db, testSecret, "garbage.token.here")
	if w.Code != 401 {
		t.Fatalf("garbage: http=%d, want 401", w.Code)
	}
	var env errEnv
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || env.Code != 10002 {
		t.Fatalf("garbage: %+v %v, want code 10002", env, err)
	}
}

func TestValidTokenSetsContext(t *testing.T) {
	db := newTestDB(t)
	u := seedUser(t, db, "alice", "user", 1, 1)
	w := serveThrough(db, testSecret, signToken(t, testSecret, validClaims(u)))
	if w.Code != 200 {
		t.Fatalf("valid: http=%d body=%s", w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["user_id"] != u.ID || got["username"] != "alice" || got["role"] != "user" ||
		got["session_id"] != "s_test" || got["device_class"] != "interactive" {
		t.Fatalf("context keys: %v", got)
	}
}

func TestBannedUser(t *testing.T) {
	db := newTestDB(t)
	u := seedUser(t, db, "banned", "user", 1, 1)
	tok := signToken(t, testSecret, validClaims(u))
	if err := db.Model(&store.User{}).Where("id = ?", u.ID).Update("status", 2).Error; err != nil {
		t.Fatal(err)
	}
	w := serveThrough(db, testSecret, tok)
	if w.Code != 403 {
		t.Fatalf("banned: http=%d, want 403", w.Code)
	}
	var env errEnv
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || env.Code != 20004 {
		t.Fatalf("banned: %+v %v, want code 20004", env, err)
	}
}

func TestTokenVersionBump(t *testing.T) {
	db := newTestDB(t)
	u := seedUser(t, db, "kicked", "user", 1, 1)
	tok := signToken(t, testSecret, validClaims(u)) // tv=1
	if err := db.Model(&store.User{}).Where("id = ?", u.ID).Update("token_version", 2).Error; err != nil {
		t.Fatal(err)
	}
	w := serveThrough(db, testSecret, tok)
	if w.Code != 401 {
		t.Fatalf("tv bump: http=%d, want 401", w.Code)
	}
	var env errEnv
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || env.Code != 10002 {
		t.Fatalf("tv bump: %+v %v, want code 10002", env, err)
	}
}

func TestWrongSecretAndAlgConfusion(t *testing.T) {
	db := newTestDB(t)
	u := seedUser(t, db, "carol", "user", 1, 1)
	// Wrong secret.
	if w := serveThrough(db, "other-secret", signToken(t, "other-secret-x", validClaims(u))); w.Code != 401 {
		t.Fatalf("wrong secret: http=%d, want 401", w.Code)
	}
	// HS384 must be rejected by the explicit HS256 check.
	claims := validClaims(u)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS384, claims)
	s384, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	if w := serveThrough(db, testSecret, s384); w.Code != 401 {
		t.Fatalf("HS384: http=%d, want 401", w.Code)
	}
	// Missing exp must be rejected.
	noExp := validClaims(u)
	delete(noExp, "exp")
	if w := serveThrough(db, testSecret, signToken(t, testSecret, noExp)); w.Code != 401 {
		t.Fatalf("no exp: http=%d, want 401", w.Code)
	}
}

func TestRevokedSessionCheck(t *testing.T) {
	db := newTestDB(t)
	u := seedUser(t, db, "dave", "user", 1, 1)
	now := time.Now()
	live := &store.UserSession{
		ID: "s_live", UserID: u.ID, DeviceClass: "interactive",
		RefreshTokenHash: "h", TokenVersion: 1,
		ExpiresAt: now.Add(time.Hour), LastActiveAt: now, CreatedAt: now,
	}
	if err := db.Create(live).Error; err != nil {
		t.Fatal(err)
	}
	rev := &store.UserSession{
		ID: "s_rev", UserID: u.ID, DeviceClass: "interactive",
		RefreshTokenHash: "h", TokenVersion: 1, IsRevoked: 1,
		ExpiresAt: now.Add(time.Hour), LastActiveAt: now, CreatedAt: now,
	}
	if err := db.Create(rev).Error; err != nil {
		t.Fatal(err)
	}

	// Live session row → allowed.
	claims := validClaims(u)
	claims["session_id"] = "s_live"
	if w := serveThrough(db, testSecret, signToken(t, testSecret, claims)); w.Code != 200 {
		t.Fatalf("live session: http=%d body=%s, want 200", w.Code, w.Body.String())
	}

	// Revoked session row → 401/10002.
	claims = validClaims(u)
	claims["session_id"] = "s_rev"
	w := serveThrough(db, testSecret, signToken(t, testSecret, claims))
	if w.Code != 401 {
		t.Fatalf("revoked session: http=%d, want 401", w.Code)
	}
	var env errEnv
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || env.Code != 10002 {
		t.Fatalf("revoked session: %+v %v, want code 10002", env, err)
	}

	// Absent session row → still allowed (pre-existing tokens keep working).
	if w := serveThrough(db, testSecret, signToken(t, testSecret, validClaims(u))); w.Code != 200 {
		t.Fatalf("absent session row: http=%d body=%s, want 200", w.Code, w.Body.String())
	}
}

func TestAdminAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		role string
		want int
	}{
		{"user", 403},
		{"admin", 200},
		{"superadmin", 200},
		{"", 403},
	} {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			if tc.role != "" {
				c.Set("role", tc.role)
			}
			c.Next()
		}, AdminAuthMiddleware())
		r.GET("/x", func(c *gin.Context) { c.Status(200) })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
		if w.Code != tc.want {
			t.Fatalf("role %q: http=%d, want %d", tc.role, w.Code, tc.want)
		}
		if tc.want == 403 {
			var env errEnv
			if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || env.Code != 10003 {
				t.Fatalf("role %q: %+v %v, want code 10003", tc.role, env, err)
			}
		}
	}
}
