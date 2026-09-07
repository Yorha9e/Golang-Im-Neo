package auth

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/config"
	"golang-im-neo-system/internal/store"
)

const testSecret = "test-secret-32chars-long-for-tests!"

func newTestService(t *testing.T) (*AuthService, *gorm.DB) {
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
	cfg := &config.Config{}
	cfg.Security.JWTSecret = testSecret
	cfg.Security.JWTAccessExpire = "2h"
	cfg.Security.JWTRefreshExpire = "336h"
	cfg.Security.BcryptCost = bcrypt.MinCost // speed up tests; prod uses 10
	return NewAuthService(db, cfg), db
}

func parseAT(t *testing.T, at string) jwt.MapClaims {
	t.Helper()
	tok, err := jwt.Parse(at, func(tk *jwt.Token) (interface{}, error) {
		return []byte(testSecret), nil
	})
	if err != nil || !tok.Valid {
		t.Fatalf("parse AT: %v valid=%v", err, tok != nil && tok.Valid)
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("claims not MapClaims")
	}
	return claims
}

func TestRegisterLoginRefreshRoundtrip(t *testing.T) {
	svc, _ := newTestService(t)

	uid, err := svc.Register("alice", "secure_password")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if uid == "" {
		t.Fatal("empty user id")
	}

	at, rt, gotUID, sid, err := svc.Login("alice", "secure_password", "interactive", "Chrome")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if gotUID != uid || at == "" || rt == "" || sid == "" {
		t.Fatalf("bad login tuple: uid=%q at=%v rt=%v sid=%q", gotUID, at != "", rt != "", sid)
	}

	claims := parseAT(t, at)
	for _, k := range []string{"user_id", "username", "role", "session_id", "device_class", "tv", "iat", "exp"} {
		if _, ok := claims[k]; !ok {
			t.Fatalf("AT missing pinned claim %q", k)
		}
	}
	if claims["user_id"] != uid || claims["username"] != "alice" || claims["role"] != "user" {
		t.Fatalf("AT identity claims wrong: %v", claims)
	}
	iat := int64(claims["iat"].(float64))
	exp := int64(claims["exp"].(float64))
	if d := time.Duration(exp-iat) * time.Second; d < 2*time.Hour-2*time.Minute || d > 2*time.Hour+2*time.Minute {
		t.Fatalf("AT lifetime not ~2h: %v", d)
	}

	nat, nrt, err := svc.Refresh(rt, sid)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if nat == "" || nrt == "" || nrt == rt {
		t.Fatal("refresh must return a fresh rotated pair")
	}
	parseAT(t, nat) // new AT must verify
}

func TestRefreshRotationReplayRejected(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Register("bob", "secure_password"); err != nil {
		t.Fatal(err)
	}
	_, rt, _, sid, err := svc.Login("bob", "secure_password", "interactive", "Chrome")
	if err != nil {
		t.Fatal(err)
	}
	_, nrt, err := svc.Refresh(rt, sid)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	// Replay the old (already rotated) RT → 10002.
	if _, _, err := svc.Refresh(rt, sid); CodeOf(err) != CodeUnauthorized {
		t.Fatalf("replay old RT: got %v, want code 10002", err)
	}
	// The rotated RT still works exactly once.
	if _, _, err := svc.Refresh(nrt, sid); err != nil {
		t.Fatalf("second refresh with rotated RT: %v", err)
	}
	if _, _, err := svc.Refresh(nrt, sid); CodeOf(err) != CodeUnauthorized {
		t.Fatalf("replay rotated RT: got %v, want code 10002", err)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Register("carol", "secure_password"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Register("carol", "another_password"); CodeOf(err) != CodeUserExisted {
		t.Fatalf("duplicate: got %v, want code 20002", err)
	}
}

func TestRegisterValidation(t *testing.T) {
	svc, _ := newTestService(t)
	for _, tc := range []struct{ u, p string }{
		{"a", "secure_password"},                           // username too short
		{string(make([]byte, 0)) + "x", "secure_password"}, // 1 char
		{"okname", "short"},                                // password too short
	} {
		if _, err := svc.Register(tc.u, tc.p); CodeOf(err) != CodeParamInvalid {
			t.Fatalf("Register(%q): got %v, want code 10001", tc.u, err)
		}
	}
}

func TestLoginWrongPassword(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Register("dave", "secure_password"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := svc.Login("dave", "wrong_password", "interactive", ""); CodeOf(err) != CodePasswordIncorrect {
		t.Fatalf("wrong password: got %v, want code 20003", err)
	}
}

func TestLoginUserNotFound(t *testing.T) {
	svc, _ := newTestService(t)
	if _, _, _, _, err := svc.Login("ghost", "whatever_password", "interactive", ""); CodeOf(err) != CodeUserNotFound {
		t.Fatalf("missing user: got %v, want code 20001", err)
	}
}

func TestLoginBanned(t *testing.T) {
	svc, db := newTestService(t)
	uid, err := svc.Register("erin", "secure_password")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.User{}).Where("id = ?", uid).Update("status", 2).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := svc.Login("erin", "secure_password", "interactive", ""); CodeOf(err) != CodeUserBanned {
		t.Fatalf("banned login: got %v, want code 20004", err)
	}
}

func TestRefreshInvalidAndExpired(t *testing.T) {
	svc, db := newTestService(t)
	if _, err := svc.Register("frank", "secure_password"); err != nil {
		t.Fatal(err)
	}
	_, rt, _, sid, err := svc.Login("frank", "secure_password", "interactive", "")
	if err != nil {
		t.Fatal(err)
	}
	// Garbage RT.
	if _, _, err := svc.Refresh("rt_garbage", sid); CodeOf(err) != CodeUnauthorized {
		t.Fatalf("garbage RT: got %v, want 10002", err)
	}
	// Unknown session.
	if _, _, err := svc.Refresh(rt, "s_does_not_exist"); CodeOf(err) != CodeUnauthorized {
		t.Fatalf("unknown session: got %v, want 10002", err)
	}
	// Expired session.
	past := time.Now().Add(-time.Hour)
	if err := db.Model(&store.UserSession{}).Where("id = ?", sid).Update("expires_at", past).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Refresh(rt, sid); CodeOf(err) != CodeUnauthorized {
		t.Fatalf("expired session: got %v, want 10002", err)
	}
}

func TestRefreshAfterBan(t *testing.T) {
	svc, db := newTestService(t)
	uid, err := svc.Register("grace", "secure_password")
	if err != nil {
		t.Fatal(err)
	}
	_, rt, _, sid, err := svc.Login("grace", "secure_password", "interactive", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.User{}).Where("id = ?", uid).Update("status", 2).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Refresh(rt, sid); CodeOf(err) != CodeUserBanned {
		t.Fatalf("refresh after ban: got %v, want 20004", err)
	}
}
