package media

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/config"
	"golang-im-neo-system/internal/store"
)

func newTestMediaService(t *testing.T) (*MediaService, *gorm.DB, *config.Config) {
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
	cfg.Security.JWTSecret = "test-secret-32chars-long-for-tests!"
	cfg.Database.Path = filepath.Join(tmp, "t.db")
	cfg.Admin.Username = "superadmin"
	cfg.Admin.Password = "SuperAdmin123!"
	cfg.Media.StorageDir = filepath.Join(tmp, "media")
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	svc := NewMediaService(db, cfg, zap.NewNop())
	return svc, db, cfg
}

func fileHeaderFor(t *testing.T, filename string, data []byte) *multipart.FileHeader {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if err := req.ParseMultipartForm(32 << 20); err != nil {
		t.Fatalf("ParseMultipartForm: %v", err)
	}
	fhs := req.MultipartForm.File["file"]
	if len(fhs) == 0 {
		t.Fatal("no file header parsed")
	}
	return fhs[0]
}

func TestAvatarAccepted(t *testing.T) {
	svc, _, _ := newTestMediaService(t)
	data := []byte("fake-png-bytes-for-avatar")
	fh := fileHeaderFor(t, "avatar.png", data)
	a, err := svc.Upload("u_alice", "avatar", "private", fh)
	if err != nil {
		t.Fatalf("Upload avatar: %v", err)
	}
	if a.MID == "" || !strings.HasPrefix(a.MID, "m_") {
		t.Fatalf("bad mid %q", a.MID)
	}
	if a.FileExt != ".png" || a.FileSize != int64(len(data)) {
		t.Fatalf("bad ext/size: %+v", a)
	}
	if a.AccessURL != "/api/v1/media/"+a.MID {
		t.Fatalf("bad access_url %q", a.AccessURL)
	}
	if a.AccessLevel != "private" || a.MediaType != "avatar" {
		t.Fatalf("bad level/type: %+v", a)
	}
	if a.FileSHA256 == "" {
		t.Fatal("empty sha256")
	}
	if _, err := os.Stat(a.StoragePath); err != nil {
		t.Fatalf("stored file missing: %v", err)
	}
	got, err := svc.Get(a.MID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.MID != a.MID || got.FileSHA256 != a.FileSHA256 {
		t.Fatalf("Get mismatch: %+v vs %+v", got, a)
	}
}

func TestAvatarTooLarge(t *testing.T) {
	svc, _, cfg := newTestMediaService(t)
	if cfg.Media.MaxAvatarBytes != 2<<20 {
		t.Fatalf("default MaxAvatarBytes = %d, want %d", cfg.Media.MaxAvatarBytes, 2<<20)
	}
	big := bytes.Repeat([]byte("x"), (2<<20)+1)
	fh := fileHeaderFor(t, "big.png", big)
	if _, err := svc.Upload("u_alice", "avatar", "private", fh); CodeOf(err) != CodeMediaTooLarge {
		t.Fatalf("oversize avatar: got %v, want code 40001", err)
	}
}

func TestAvatarBadExt(t *testing.T) {
	svc, _, _ := newTestMediaService(t)
	for _, name := range []string{"evil.exe", "note.txt"} {
		fh := fileHeaderFor(t, name, []byte("payload-bytes"))
		if _, err := svc.Upload("u_alice", "avatar", "private", fh); CodeOf(err) != CodeMediaTypeNotAllow {
			t.Fatalf("Upload %s: got %v, want code 40002", name, err)
		}
	}
}

func TestValidWhitelistExtAccepted(t *testing.T) {
	svc, _, _ := newTestMediaService(t)
	cases := []struct {
		mediaType string
		filename  string
	}{
		{"avatar", "a.jpg"},
		{"avatar", "b.jpeg"},
		{"avatar", "c.png"},
		{"avatar", "d.gif"},
		{"avatar", "e.webp"},
		{"image", "f.jpg"},
		{"voice", "g.mp3"},
		{"video", "h.mp4"},
	}
	for i, tc := range cases {
		data := []byte(strings.Repeat("v", 64) + string(rune('a'+i)) + tc.filename)
		fh := fileHeaderFor(t, tc.filename, data)
		if _, err := svc.Upload("u_alice", tc.mediaType, "public", fh); err != nil {
			t.Fatalf("Upload %s/%s: %v", tc.mediaType, tc.filename, err)
		}
	}
}

func TestDedupSameBytesSameMid(t *testing.T) {
	svc, db, cfg := newTestMediaService(t)
	data := []byte("dedup-payload-bytes-unique-001")
	a1, err := svc.Upload("u_alice", "image", "private", fileHeaderFor(t, "a.png", data))
	if err != nil {
		t.Fatalf("first upload: %v", err)
	}
	a2, err := svc.Upload("u_alice", "image", "private", fileHeaderFor(t, "a.png", data))
	if err != nil {
		t.Fatalf("second upload: %v", err)
	}
	if a1.MID != a2.MID {
		t.Fatalf("dedup: mids differ %q vs %q", a1.MID, a2.MID)
	}
	var n int64
	if err := db.Model(&store.MediaAsset{}).Where("file_sha256 = ?", a1.FileSHA256).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("dedup rows = %d, want 1", n)
	}
	entries, err := os.ReadDir(cfg.Media.StorageDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dedup files = %d, want 1", len(entries))
	}
}

func TestDedupDifferentUploaderNewAsset(t *testing.T) {
	svc, _, _ := newTestMediaService(t)
	data := []byte("shared-bytes-across-users-002")
	a1, err := svc.Upload("u_alice", "image", "private", fileHeaderFor(t, "a.png", data))
	if err != nil {
		t.Fatal(err)
	}
	a2, err := svc.Upload("u_bob", "image", "private", fileHeaderFor(t, "a.png", data))
	if err != nil {
		t.Fatal(err)
	}
	if a1.MID == a2.MID {
		t.Fatal("different uploaders must not dedup to the same mid")
	}
}

func TestGetNotFound(t *testing.T) {
	svc, _, _ := newTestMediaService(t)
	if _, err := svc.Get("m_does_not_exist"); CodeOf(err) != CodeMediaNotFound {
		t.Fatalf("Get missing: got %v, want code 20001", err)
	}
	if _, err := svc.Get(""); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("Get empty: got %v, want code 10001", err)
	}
}

func TestUploadValidation(t *testing.T) {
	svc, _, _ := newTestMediaService(t)
	fh := fileHeaderFor(t, "a.png", []byte("bytes"))
	if _, err := svc.Upload("", "avatar", "private", fh); CodeOf(err) != CodeUnauthorized {
		t.Fatalf("empty uploader: got %v, want 10002", err)
	}
	if _, err := svc.Upload("u_a", "", "private", fh); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("empty media_type: got %v, want 10001", err)
	}
	if _, err := svc.Upload("u_a", "document", "private", fh); CodeOf(err) != CodeMediaTypeNotAllow {
		t.Fatalf("bad media_type: got %v, want 40002", err)
	}
	if _, err := svc.Upload("u_a", "avatar", "private", nil); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("nil file: got %v, want 10001", err)
	}
	if _, err := svc.Upload("u_a", "avatar", "weird", fh); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("bad access_level: got %v, want 10001", err)
	}
	// Default access level is private.
	a, err := svc.Upload("u_a", "avatar", "", fileHeaderFor(t, "d.png", []byte("default-level-bytes")))
	if err != nil {
		t.Fatalf("default level upload: %v", err)
	}
	if a.AccessLevel != "private" {
		t.Fatalf("default access_level = %q, want private", a.AccessLevel)
	}
}

func TestNonAvatarSizeLimit(t *testing.T) {
	svc, _, cfg := newTestMediaService(t)
	cfg.Media.MaxFileBytes = 16
	fh := fileHeaderFor(t, "big.jpg", bytes.Repeat([]byte("y"), 32))
	if _, err := svc.Upload("u_alice", "image", "private", fh); CodeOf(err) != CodeMediaTooLarge {
		t.Fatalf("non-avatar oversize: got %v, want 40001", err)
	}
}

func TestConfigDefaultsWhenMediaMissing(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Port = 8080
	cfg.Security.JWTSecret = "test-secret-32chars-long-for-tests!"
	cfg.Database.Path = filepath.Join(t.TempDir(), "t.db")
	cfg.Admin.Username = "superadmin"
	cfg.Admin.Password = "SuperAdmin123!"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.Media.StorageDir != "./data/media" {
		t.Fatalf("StorageDir = %q, want ./data/media", cfg.Media.StorageDir)
	}
	if cfg.Media.MaxAvatarBytes != 2<<20 {
		t.Fatalf("MaxAvatarBytes = %d, want %d", cfg.Media.MaxAvatarBytes, 2<<20)
	}
	if cfg.Media.MaxFileBytes != 20<<20 {
		t.Fatalf("MaxFileBytes = %d, want %d", cfg.Media.MaxFileBytes, 20<<20)
	}
	if !strings.Contains(cfg.Media.AllowedAvatarExts, ".jpg") || !strings.Contains(cfg.Media.AllowedAvatarExts, ".webp") {
		t.Fatalf("AllowedAvatarExts = %q, want image whitelist", cfg.Media.AllowedAvatarExts)
	}
	if !strings.Contains(cfg.Media.AllowedFileExts, ".mp3") || !strings.Contains(cfg.Media.AllowedFileExts, ".mp4") {
		t.Fatalf("AllowedFileExts = %q, want audio/video entries", cfg.Media.AllowedFileExts)
	}
}

func TestResolvePathTraversalGuard(t *testing.T) {
	svc, _, cfg := newTestMediaService(t)
	a, err := svc.Upload("u_alice", "image", "private", fileHeaderFor(t, "a.png", []byte("traversal-guard-bytes")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolvePath(a); err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}
	evil := *a
	evil.StoragePath = filepath.Join(cfg.Media.StorageDir, "..", "evil.txt")
	if _, err := svc.ResolvePath(&evil); CodeOf(err) != CodeInternalServer {
		t.Fatalf("traversal path: got %v, want 50001", err)
	}
	absEvil := *a
	absEvil.StoragePath = filepath.Join(string(filepath.Separator), "tmp", "evil.txt")
	if _, err := svc.ResolvePath(&absEvil); CodeOf(err) != CodeInternalServer {
		t.Fatalf("absolute outside path: got %v, want 50001", err)
	}
}
