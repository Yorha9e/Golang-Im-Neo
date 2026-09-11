package media

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"golang-im-neo-system/internal/config"
	"golang-im-neo-system/internal/logic/auth"
	"golang-im-neo-system/internal/middleware"
	"golang-im-neo-system/internal/store"
)

const testJWTSecret = "test-secret-32chars-long-for-tests!"

type httpEnv struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

type uploadData struct {
	MID         string `json:"mid"`
	AccessURL   string `json:"access_url"`
	MediaType   string `json:"media_type"`
	AccessLevel string `json:"access_level"`
	FileSize    int64  `json:"file_size"`
	FileExt     string `json:"file_ext"`
	SHA256      string `json:"sha256"`
}

func newMediaTestRouter(t *testing.T) (*gin.Engine, string, string) {
	t.Helper()
	tmp := t.TempDir()
	db, err := store.NewDB(store.DBConfig{Path: filepath.Join(tmp, "t.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	cfg := &config.Config{}
	cfg.Server.Port = 8080
	cfg.Security.JWTSecret = testJWTSecret
	cfg.Security.JWTAccessExpire = "2h"
	cfg.Security.JWTRefreshExpire = "336h"
	cfg.Security.BcryptCost = bcrypt.MinCost
	cfg.Database.Path = filepath.Join(tmp, "t.db")
	cfg.Admin.Username = "superadmin"
	cfg.Admin.Password = "SuperAdmin123!"
	cfg.Media.StorageDir = filepath.Join(tmp, "media")
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	authSvc := auth.NewAuthService(db, cfg)
	if _, err := authSvc.Register("uploader1", "secure_password"); err != nil {
		t.Fatalf("register uploader: %v", err)
	}
	if _, err := authSvc.Register("otheruser1", "secure_password"); err != nil {
		t.Fatalf("register other: %v", err)
	}
	uploaderAT, _, _, _, err := authSvc.Login("uploader1", "secure_password", "interactive", "Chrome")
	if err != nil {
		t.Fatalf("login uploader: %v", err)
	}
	otherAT, _, _, _, err := authSvc.Login("otheruser1", "secure_password", "interactive", "Chrome")
	if err != nil {
		t.Fatalf("login other: %v", err)
	}

	svc := NewMediaService(db, cfg, zap.NewNop())
	gin.SetMode(gin.TestMode)
	r := gin.New()
	rg := r.Group("/api/v1/media")
	RegisterRoutes(rg, svc, middleware.JWTAuthMiddleware(testJWTSecret, db))
	return r, uploaderAT, otherAT
}

func doUpload(t *testing.T, r http.Handler, token, filename, mediaType, accessLevel string, data []byte) (int, httpEnv, uploadData) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if data != nil {
		fw, err := w.CreateFormFile("file", filename)
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	if mediaType != "" {
		_ = w.WriteField("media_type", mediaType)
	}
	if accessLevel != "" {
		_ = w.WriteField("access_level", accessLevel)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/media/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var env httpEnv
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("POST upload: bad envelope: %v (%s)", err, rec.Body.String())
	}
	var ud uploadData
	if env.Code == 0 && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, &ud); err != nil {
			t.Fatalf("upload data decode: %v (%s)", err, string(env.Data))
		}
	}
	return rec.Code, env, ud
}

func doGet(t *testing.T, r http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestHTTPPublicReadableAnonymously(t *testing.T) {
	r, uploaderAT, _ := newMediaTestRouter(t)
	payload := []byte("public-image-payload-001")
	httpCode, env, ud := doUpload(t, r, uploaderAT, "pub.png", "image", "public", payload)
	if httpCode != 200 || env.Code != 0 {
		t.Fatalf("upload public: http=%d env=%+v", httpCode, env)
	}
	if ud.MID == "" || ud.AccessURL != "/api/v1/media/"+ud.MID {
		t.Fatalf("bad upload data: %+v", ud)
	}
	if ud.MediaType != "image" || ud.AccessLevel != "public" || ud.FileExt != ".png" || ud.SHA256 == "" {
		t.Fatalf("bad upload fields: %+v", ud)
	}
	// Anonymous read must succeed.
	rec := doGet(t, r, "/api/v1/media/"+ud.MID, "")
	if rec.Code != 200 {
		t.Fatalf("public anon GET: http=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), payload) {
		t.Fatal("public anon GET bytes mismatch")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Fatalf("public Content-Type = %q, want image/png", ct)
	}
	// Verify CDN / edge caching headers on public asset
	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "public") || !strings.Contains(cc, "immutable") {
		t.Fatalf("public Cache-Control = %q, want public, max-age=2592000, immutable", cc)
	}
	if etag := rec.Header().Get("ETag"); etag == "" {
		t.Fatal("public ETag missing")
	}
}

func TestHTTPPrivateAccessIsolation(t *testing.T) {
	r, uploaderAT, otherAT := newMediaTestRouter(t)
	payload := []byte("private-image-payload-002")
	_, env, ud := doUpload(t, r, uploaderAT, "priv.png", "image", "private", payload)
	if env.Code != 0 {
		t.Fatalf("upload private: %+v", env)
	}
	// Anonymous → 401/10002.
	rec := doGet(t, r, "/api/v1/media/"+ud.MID, "")
	if rec.Code != 401 {
		t.Fatalf("private anon GET: http=%d body=%s, want 401", rec.Code, rec.Body.String())
	}
	var anonEnv httpEnv
	if err := json.Unmarshal(rec.Body.Bytes(), &anonEnv); err != nil || anonEnv.Code != CodeUnauthorized {
		t.Fatalf("private anon GET env: %v %+v, want code 10002", err, anonEnv)
	}
	// Non-uploader with JWT → 403/10003.
	rec = doGet(t, r, "/api/v1/media/"+ud.MID, otherAT)
	if rec.Code != 403 {
		t.Fatalf("private other GET: http=%d body=%s, want 403", rec.Code, rec.Body.String())
	}
	var otherEnv httpEnv
	if err := json.Unmarshal(rec.Body.Bytes(), &otherEnv); err != nil || otherEnv.Code != CodeForbidden {
		t.Fatalf("private other GET env: %v %+v, want code 10003", err, otherEnv)
	}
	// Uploader with JWT → 200 + bytes.
	rec = doGet(t, r, "/api/v1/media/"+ud.MID, uploaderAT)
	if rec.Code != 200 {
		t.Fatalf("private owner GET: http=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), payload) {
		t.Fatal("private owner GET bytes mismatch")
	}
}

func TestHTTPUploadValidation(t *testing.T) {
	r, uploaderAT, _ := newMediaTestRouter(t)

	// Avatar >2MB → 40001.
	big := bytes.Repeat([]byte("x"), (2<<20)+1)
	httpCode, env, _ := doUpload(t, r, uploaderAT, "big.png", "avatar", "private", big)
	if env.Code != CodeMediaTooLarge || httpCode == 200 {
		t.Fatalf("oversize avatar: http=%d env=%+v, want 40001 non-200", httpCode, env)
	}
	// Avatar bad ext → 40002.
	httpCode, env, _ = doUpload(t, r, uploaderAT, "evil.exe", "avatar", "private", []byte("bytes"))
	if env.Code != CodeMediaTypeNotAllow || httpCode == 200 {
		t.Fatalf("bad ext avatar: http=%d env=%+v, want 40002 non-200", httpCode, env)
	}
	// Missing file → 10001.
	httpCode, env, _ = doUpload(t, r, uploaderAT, "", "avatar", "private", nil)
	if env.Code != CodeParamInvalid || httpCode == 200 {
		t.Fatalf("missing file: http=%d env=%+v, want 10001 non-200", httpCode, env)
	}
	// No token → 401/10002.
	httpCode, env, _ = doUpload(t, r, "", "a.png", "avatar", "private", []byte("bytes"))
	if httpCode != 401 || env.Code != CodeUnauthorized {
		t.Fatalf("no-auth upload: http=%d env=%+v, want 401/10002", httpCode, env)
	}
	// Missing media → 20001/404.
	rec := doGet(t, r, "/api/v1/media/m_does_not_exist", uploaderAT)
	if rec.Code != 404 {
		t.Fatalf("missing GET: http=%d body=%s, want 404", rec.Code, rec.Body.String())
	}
	var missEnv httpEnv
	if err := json.Unmarshal(rec.Body.Bytes(), &missEnv); err != nil || missEnv.Code != CodeMediaNotFound {
		t.Fatalf("missing GET env: %v %+v, want 20001", err, missEnv)
	}
}

func TestHTTPDedupSameMid(t *testing.T) {
	r, uploaderAT, _ := newMediaTestRouter(t)
	payload := []byte("http-dedup-payload-003")
	_, env1, ud1 := doUpload(t, r, uploaderAT, "dup.png", "image", "private", payload)
	_, env2, ud2 := doUpload(t, r, uploaderAT, "dup.png", "image", "private", payload)
	if env1.Code != 0 || env2.Code != 0 {
		t.Fatalf("dedup uploads: %+v %+v", env1, env2)
	}
	if ud1.MID != ud2.MID {
		t.Fatalf("http dedup: mids differ %q vs %q", ud1.MID, ud2.MID)
	}
}
