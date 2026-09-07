package profile

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	"golang-im-neo-system/internal/store"
)

// ---------- helpers ----------

func newProfileTestService(t *testing.T, resolver LinkPreviewResolver) (*ProfileService, *gorm.DB) {
	t.Helper()
	db, err := store.NewDB(store.DBConfig{Path: filepath.Join(t.TempDir(), "p.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	if resolver == nil {
		resolver = NoopResolver{}
	}
	return NewProfileService(db, resolver, nil), db
}

func createProfileUser(t *testing.T, db *gorm.DB, id, username string) {
	t.Helper()
	u := &store.User{
		ID:           id,
		Username:     username,
		PasswordHash: "hash",
		Role:         "user",
		Status:       1,
		TokenVersion: 1,
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("create user %q: %v", username, err)
	}
}

func strptr(s string) *string { return &s }

type stubResolver struct {
	preview *LinkPreview
	err     error
	calls   int
	lastURL string
}

func (s *stubResolver) Resolve(_ context.Context, rawURL string) (*LinkPreview, error) {
	s.calls++
	s.lastURL = rawURL
	if s.err != nil {
		return nil, s.err
	}
	return s.preview, nil
}

func mustCreateTextPost(t *testing.T, svc *ProfileService, userID, content string) *PostDTO {
	t.Helper()
	dto, err := svc.CreatePost(userID, CreatePostInput{MediaType: "text", Content: content})
	if err != nil {
		t.Fatalf("CreatePost text: %v", err)
	}
	return dto
}

// ---------- public profile ----------

func TestGetPublicProfileUnknown(t *testing.T) {
	svc, _ := newProfileTestService(t, nil)
	if _, err := svc.GetPublicProfile("ghost"); CodeOf(err) != CodeUserNotFound {
		t.Fatalf("unknown user: got %v, want 20001", err)
	}
	if got := HTTPStatusOf(CodeOf(errors.New("x"))); got != 500 {
		t.Fatalf("unknown err status = %d, want 500", got)
	}
	if _, err := svc.GetPublicProfile("  "); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("blank username: got %v, want 10001", err)
	}
}

func TestGetPublicProfileCardAndNewestFirst(t *testing.T) {
	svc, db := newProfileTestService(t, nil)
	createProfileUser(t, db, "u_alice", "alice")
	if err := db.Model(&store.User{}).Where("id = ?", "u_alice").Updates(map[string]interface{}{
		"nickname": "Ali", "avatar_url": "http://x/a.png", "signature": "hello",
	}).Error; err != nil {
		t.Fatal(err)
	}
	first := mustCreateTextPost(t, svc, "u_alice", "one")
	second := mustCreateTextPost(t, svc, "u_alice", "two")
	third := mustCreateTextPost(t, svc, "u_alice", "three")

	pp, err := svc.GetPublicProfile("alice")
	if err != nil {
		t.Fatalf("GetPublicProfile: %v", err)
	}
	if pp.User.Username != "alice" || pp.User.Nickname != "Ali" ||
		pp.User.AvatarURL != "http://x/a.png" || pp.User.Signature != "hello" {
		t.Fatalf("card wrong: %+v", pp.User)
	}
	if len(pp.Posts) != 3 {
		t.Fatalf("posts = %d, want 3", len(pp.Posts))
	}
	// Newest first.
	if pp.Posts[0].ID != third.ID || pp.Posts[1].ID != second.ID || pp.Posts[2].ID != first.ID {
		t.Fatalf("order wrong: %d %d %d, want %d %d %d (newest first)",
			pp.Posts[0].ID, pp.Posts[1].ID, pp.Posts[2].ID, third.ID, second.ID, first.ID)
	}
	for i, p := range pp.Posts {
		if p.MediaType != "text" || p.LinkPreview != nil || p.CreatedAt <= 0 {
			t.Fatalf("post[%d] wrong: %+v", i, p)
		}
	}
}

// ---------- update ----------

func TestUpdateProfilePartialAndEmptyString(t *testing.T) {
	svc, db := newProfileTestService(t, nil)
	createProfileUser(t, db, "u_alice", "alice")
	if err := db.Model(&store.User{}).Where("id = ?", "u_alice").Updates(map[string]interface{}{
		"nickname": "Ali", "avatar_url": "http://x/a.png", "signature": "hello",
	}).Error; err != nil {
		t.Fatal(err)
	}
	// Partial: only nickname changes.
	card, err := svc.UpdateProfile("u_alice", UpdateProfileInput{Nickname: strptr("Alicia")})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if card.Nickname != "Alicia" || card.AvatarURL != "http://x/a.png" || card.Signature != "hello" || card.Username != "alice" {
		t.Fatalf("partial update wrong: %+v", card)
	}
	// Empty string applies (clears the field), others untouched.
	card, err = svc.UpdateProfile("u_alice", UpdateProfileInput{Signature: strptr("")})
	if err != nil {
		t.Fatal(err)
	}
	if card.Signature != "" || card.Nickname != "Alicia" || card.AvatarURL != "http://x/a.png" {
		t.Fatalf("empty-string update wrong: %+v", card)
	}
	// No-op update returns current card.
	card, err = svc.UpdateProfile("u_alice", UpdateProfileInput{})
	if err != nil {
		t.Fatal(err)
	}
	if card.Nickname != "Alicia" {
		t.Fatalf("noop update wrong: %+v", card)
	}
}

func TestUpdateProfileLengthValidation(t *testing.T) {
	svc, db := newProfileTestService(t, nil)
	createProfileUser(t, db, "u_alice", "alice")
	if _, err := svc.UpdateProfile("u_alice", UpdateProfileInput{Nickname: strptr(strings.Repeat("n", 65))}); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("long nickname: got %v, want 10001", err)
	}
	if _, err := svc.UpdateProfile("u_alice", UpdateProfileInput{Signature: strptr(strings.Repeat("s", 256))}); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("long signature: got %v, want 10001", err)
	}
	if _, err := svc.UpdateProfile("u_alice", UpdateProfileInput{AvatarURL: strptr(strings.Repeat("a", 256))}); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("long avatar: got %v, want 10001", err)
	}
	// Boundary values are accepted.
	if _, err := svc.UpdateProfile("u_alice", UpdateProfileInput{Nickname: strptr(strings.Repeat("n", 64))}); err != nil {
		t.Fatalf("nickname len 64: %v", err)
	}
	if _, err := svc.UpdateProfile("u_alice", UpdateProfileInput{Signature: strptr(strings.Repeat("s", 255))}); err != nil {
		t.Fatalf("signature len 255: %v", err)
	}
	if _, err := svc.UpdateProfile("u_alice", UpdateProfileInput{AvatarURL: strptr(strings.Repeat("a", 255))}); err != nil {
		t.Fatalf("avatar len 255: %v", err)
	}
	if _, err := svc.UpdateProfile("no-such-user", UpdateProfileInput{Nickname: strptr("x")}); CodeOf(err) != CodeUserNotFound {
		t.Fatalf("unknown user update: got %v, want 20001", err)
	}
}

// ---------- create ----------

func TestCreatePostHappyPaths(t *testing.T) {
	svc, db := newProfileTestService(t, nil)
	createProfileUser(t, db, "u_alice", "alice")

	dto, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "text", Content: "hello"})
	if err != nil || dto.ID == 0 || dto.MediaType != "text" || dto.Content != "hello" || dto.CreatedAt <= 0 {
		t.Fatalf("text post: %+v err=%v", dto, err)
	}
	dto, err = svc.CreatePost("u_alice", CreatePostInput{MediaType: "image", Content: "pic", MediaURL: "http://x/i.png"})
	if err != nil || dto.MediaURL != "http://x/i.png" {
		t.Fatalf("image post: %+v err=%v", dto, err)
	}
	dto, err = svc.CreatePost("u_alice", CreatePostInput{MediaType: "video", MediaURL: "http://x/v.mp4"})
	if err != nil || dto.MediaType != "video" {
		t.Fatalf("video post: %+v err=%v", dto, err)
	}
	dto, err = svc.CreatePost("u_alice", CreatePostInput{MediaType: "bilibili", BilibiliBVID: "BV1xx411c7mD"})
	if err != nil || dto.BilibiliBVID != "BV1xx411c7mD" {
		t.Fatalf("bilibili post: %+v err=%v", dto, err)
	}
	// NoopResolver stores nothing for bilibili.
	if dto.LinkPreview != nil {
		t.Fatalf("noop resolver must store nothing, got %+v", dto.LinkPreview)
	}
}

func TestCreatePostValidationMatrix(t *testing.T) {
	svc, db := newProfileTestService(t, nil)
	createProfileUser(t, db, "u_alice", "alice")

	if _, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "audio"}); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("bad media_type: got %v, want 10001", err)
	}
	if _, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "bilibili", BilibiliBVID: "bad"}); CodeOf(err) != CodeBilibiliInvalid {
		t.Fatalf("bad bvid: got %v, want 40003", err)
	}
	if _, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "bilibili"}); CodeOf(err) != CodeBilibiliInvalid {
		t.Fatalf("missing bvid: got %v, want 40003", err)
	}
	if _, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "bilibili", BilibiliBVID: "BVshort"}); CodeOf(err) != CodeBilibiliInvalid {
		t.Fatalf("short bvid: got %v, want 40003", err)
	}
	if _, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "text", Content: "   "}); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("empty text: got %v, want 10001", err)
	}
	if _, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "text", Content: "hi", MediaURL: "http://x/i.png"}); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("text with media_url: got %v, want 10001", err)
	}
	if _, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "image"}); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("image w/o media_url: got %v, want 10001", err)
	}
	if _, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "video", Content: "x"}); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("video w/o media_url: got %v, want 10001", err)
	}
	if _, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "text", Content: strings.Repeat("c", 2001)}); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("long content: got %v, want 10001", err)
	}
	// Caller preview with non-http target.
	badPreview := &LinkPreview{Title: "t", TargetURL: "ftp://x/y", Site: "web"}
	if _, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "text", Content: "hi", LinkPreview: badPreview}); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("bad preview url: got %v, want 10001", err)
	}
	emptyTarget := &LinkPreview{Title: "t", Site: "web"}
	if _, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "text", Content: "hi", LinkPreview: emptyTarget}); CodeOf(err) != CodeParamInvalid {
		t.Fatalf("empty preview url: got %v, want 10001", err)
	}
}

func TestCreatePostCallerPreviewRoundTrip(t *testing.T) {
	svc, db := newProfileTestService(t, nil)
	createProfileUser(t, db, "u_alice", "alice")
	want := &LinkPreview{Title: "T", CoverURL: "http://x/c.png", TargetURL: "https://example.com/a", Site: "example"}
	dto, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "text", Content: "with link", LinkPreview: want})
	if err != nil {
		t.Fatalf("CreatePost with preview: %v", err)
	}
	if dto.LinkPreview == nil || *dto.LinkPreview != *want {
		t.Fatalf("dto preview = %+v, want %+v", dto.LinkPreview, want)
	}
	// Raw DB column round-trips.
	var row store.UserPost
	if err := db.Where("id = ?", dto.ID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.LinkPreviewJSON == "" {
		t.Fatal("link_preview_json not persisted")
	}
	got := parseLinkPreview(row.LinkPreviewJSON)
	if got == nil || *got != *want {
		t.Fatalf("db preview = %+v, want %+v", got, want)
	}
	// Visible through the public profile JSON shape.
	pp, err := svc.GetPublicProfile("alice")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range pp.Posts {
		if p.ID == dto.ID {
			found = true
			if p.LinkPreview == nil || *p.LinkPreview != *want {
				t.Fatalf("profile preview = %+v, want %+v", p.LinkPreview, want)
			}
		}
	}
	if !found {
		t.Fatal("created post missing from public profile")
	}
}

func TestCreatePostResolverPersisted(t *testing.T) {
	stub := &stubResolver{preview: &LinkPreview{Title: "BV title", TargetURL: "https://www.bilibili.com/video/BV1xx411c7mD", Site: "bilibili"}}
	svc, db := newProfileTestService(t, stub)
	createProfileUser(t, db, "u_alice", "alice")
	dto, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "bilibili", BilibiliBVID: "BV1xx411c7mD"})
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	if stub.calls != 1 || stub.lastURL != "https://www.bilibili.com/video/BV1xx411c7mD" {
		t.Fatalf("resolver not called with bvid url: calls=%d last=%q", stub.calls, stub.lastURL)
	}
	if dto.LinkPreview == nil || dto.LinkPreview.Title != "BV title" {
		t.Fatalf("resolved preview not persisted: %+v", dto.LinkPreview)
	}
}

func TestCreatePostResolverErrorDoesNotFail(t *testing.T) {
	stub := &stubResolver{err: errors.New("boom")}
	svc, db := newProfileTestService(t, stub)
	createProfileUser(t, db, "u_alice", "alice")
	dto, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "bilibili", BilibiliBVID: "BV1xx411c7mD"})
	if err != nil {
		t.Fatalf("resolver error must not fail creation: %v", err)
	}
	if dto.LinkPreview != nil {
		t.Fatalf("failed resolve must store empty, got %+v", dto.LinkPreview)
	}
	if stub.calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", stub.calls)
	}
}

func TestCreatePostNilResolverStoresNothing(t *testing.T) {
	db, err := store.NewDB(store.DBConfig{Path: filepath.Join(t.TempDir(), "nil.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	svc := NewProfileService(db, nil, nil)
	createProfileUser(t, db, "u_alice", "alice")
	dto, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "bilibili", BilibiliBVID: "BV1xx411c7mD"})
	if err != nil {
		t.Fatalf("CreatePost nil resolver: %v", err)
	}
	if dto.LinkPreview != nil {
		t.Fatalf("nil resolver must store nothing, got %+v", dto.LinkPreview)
	}
}

// ---------- delete + card ----------

func TestDeletePostOwnerNonOwnerMissing(t *testing.T) {
	svc, db := newProfileTestService(t, nil)
	createProfileUser(t, db, "u_alice", "alice")
	createProfileUser(t, db, "u_bob", "bob")
	dto := mustCreateTextPost(t, svc, "u_alice", "bye")

	if err := svc.DeletePost("u_bob", dto.ID); CodeOf(err) != CodeForbidden {
		t.Fatalf("non-owner delete: got %v, want 10003", err)
	}
	if err := svc.DeletePost("u_alice", 999999); CodeOf(err) != CodePostNotFound {
		t.Fatalf("missing delete: got %v, want 40004", err)
	}
	if err := svc.DeletePost("u_alice", dto.ID); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	// Second delete → 40004 (already soft-deleted).
	if err := svc.DeletePost("u_alice", dto.ID); CodeOf(err) != CodePostNotFound {
		t.Fatalf("double delete: got %v, want 40004", err)
	}
	// Card and public profile agree the post is gone.
	if _, err := svc.GetPostCard(dto.ID); CodeOf(err) != CodePostNotFound {
		t.Fatalf("card after delete: got %v, want 40004", err)
	}
	pp, err := svc.GetPublicProfile("alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pp.Posts {
		if p.ID == dto.ID {
			t.Fatalf("deleted post still listed: %+v", p)
		}
	}
}

func TestGetPostCardSynthesizedBilibili(t *testing.T) {
	svc, db := newProfileTestService(t, nil)
	createProfileUser(t, db, "u_alice", "alice")
	dto, err := svc.CreatePost("u_alice", CreatePostInput{MediaType: "bilibili", BilibiliBVID: "BV1xx411c7mD"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := svc.GetPostCard(dto.ID)
	if err != nil {
		t.Fatalf("GetPostCard: %v", err)
	}
	if card.PostID != dto.ID || card.MediaType != "bilibili" {
		t.Fatalf("card identity wrong: %+v", card)
	}
	if card.LinkPreview == nil ||
		card.LinkPreview.TargetURL != "https://www.bilibili.com/video/BV1xx411c7mD" ||
		card.LinkPreview.Site != "bilibili" {
		t.Fatalf("synthesized jump target wrong: %+v", card.LinkPreview)
	}
}

func TestGetPostCardStoredPreviewWins(t *testing.T) {
	svc, db := newProfileTestService(t, nil)
	createProfileUser(t, db, "u_alice", "alice")
	want := &LinkPreview{Title: "T", TargetURL: "https://example.com/v", Site: "example"}
	dto, err := svc.CreatePost("u_alice", CreatePostInput{
		MediaType: "bilibili", BilibiliBVID: "BV1xx411c7mD", LinkPreview: want,
	})
	if err != nil {
		t.Fatal(err)
	}
	card, err := svc.GetPostCard(dto.ID)
	if err != nil {
		t.Fatal(err)
	}
	if card.LinkPreview == nil || *card.LinkPreview != *want {
		t.Fatalf("stored preview must win: %+v, want %+v", card.LinkPreview, want)
	}
	// Plain text post without preview → null link_preview.
	txt := mustCreateTextPost(t, svc, "u_alice", "plain")
	card, err = svc.GetPostCard(txt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if card.LinkPreview != nil {
		t.Fatalf("text card preview must be null, got %+v", card.LinkPreview)
	}
}

func TestGetPostCardMissing(t *testing.T) {
	svc, _ := newProfileTestService(t, nil)
	if _, err := svc.GetPostCard(999999); CodeOf(err) != CodePostNotFound {
		t.Fatalf("missing card: got %v, want 40004", err)
	}
	if _, err := svc.GetPostCard(0); CodeOf(err) != CodePostNotFound {
		t.Fatalf("zero card: got %v, want 40004", err)
	}
}

func TestHTTPStatusMapping(t *testing.T) {
	cases := map[int]int{
		CodeParamInvalid:    400,
		CodeUnauthorized:    401,
		CodeForbidden:       403,
		CodeUserNotFound:    404,
		CodeBilibiliInvalid: 400,
		CodePostNotFound:    404,
		CodeInternal:        500,
	}
	for code, want := range cases {
		if got := HTTPStatusOf(code); got != want {
			t.Fatalf("HTTPStatusOf(%d) = %d, want %d", code, got, want)
		}
	}
}
