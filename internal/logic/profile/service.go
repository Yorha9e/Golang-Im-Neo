// Package profile implements the profile-domain business logic (M1).
//
// SSOT: error codes follow the same style as internal/logic/friend
// (typed ProfileError, CodeOf, HTTPStatusOf); the HTTP adapter lives in
// http.go and this file holds pure business logic (no gin imports).
//
// Link-preview reservation: the data shape (LinkPreview, persisted as
// UserPost.LinkPreviewJSON) and the jump API (GetPostCard) are 100% ready.
// Heavy URL scraping is deferred via the pluggable LinkPreviewResolver slot;
// the default NoopResolver stores nothing.
package profile

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/store"
)

// SSOT error codes (same ranges as friend).
const (
	CodeSuccess         = 0
	CodeParamInvalid    = 10001 // ERR_PARAM_INVALID
	CodeUnauthorized    = 10002 // ERR_UNAUTHORIZED
	CodeForbidden       = 10003 // ERR_FORBIDDEN
	CodeUserNotFound    = 20001 // ERR_USER_NOT_FOUND
	CodeBilibiliInvalid = 40003 // ERR_BILIBILI_INVALID_BVID
	CodePostNotFound    = 40004 // ERR_POST_NOT_FOUND
	CodeInternal        = 50001 // ERR_INTERNAL_SERVER
)

// ProfileError is a typed business error carrying an SSOT code.
type ProfileError struct {
	Code int
	Msg  string
}

func (e *ProfileError) Error() string { return e.Msg }

// NewProfileError builds a typed SSOT error.
func NewProfileError(code int, msg string) *ProfileError { return &ProfileError{Code: code, Msg: msg} }

// CodeOf unwraps the SSOT code from err; unknown errors map to 50001.
func CodeOf(err error) int {
	var pe *ProfileError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return CodeInternal
}

// HTTPStatusOf maps an SSOT code to a proper HTTP status.
// A non-zero business code is NEVER returned with HTTP 200.
func HTTPStatusOf(code int) int {
	switch code {
	case CodeSuccess:
		return 200
	case CodeParamInvalid:
		return 400
	case CodeUnauthorized:
		return 401
	case CodeForbidden:
		return 403
	case CodeUserNotFound:
		return 404
	case CodeBilibiliInvalid:
		return 400
	case CodePostNotFound:
		return 404
	case CodeInternal:
		return 500
	default:
		if code >= 50000 {
			return 500
		}
		if code >= 40000 {
			return 400
		}
		return 400
	}
}

// LinkPreview is the reserved link-preview data shape. Clients open
// TargetURL directly (jump API); Title/CoverURL enrich the card when known.
type LinkPreview struct {
	Title     string `json:"title"`
	CoverURL  string `json:"cover_url"`
	TargetURL string `json:"target_url"`
	Site      string `json:"site"`
}

// LinkPreviewResolver is the pluggable metadata-parser slot. Heavy scraping
// (fetching remote pages, parsing OpenGraph, etc.) is deferred; implementors
// plug a real scraper here later without changing the service or API.
type LinkPreviewResolver interface {
	Resolve(ctx context.Context, rawURL string) (*LinkPreview, error)
}

// NoopResolver defers heavy scraping: it resolves nothing and stores
// nothing. Pass nil (treated as noop) or &NoopResolver{} by default.
type NoopResolver struct{}

// Resolve implements LinkPreviewResolver by returning (nil, nil).
func (NoopResolver) Resolve(_ context.Context, _ string) (*LinkPreview, error) {
	return nil, nil
}

// ProfileService is the pure profile business logic.
type ProfileService struct {
	db       *gorm.DB
	resolver LinkPreviewResolver
	logger   *zap.Logger
}

// NewProfileService builds the service. A nil resolver is fine (treated as
// noop, i.e. no previews are auto-resolved).
func NewProfileService(db *gorm.DB, resolver LinkPreviewResolver, logger *zap.Logger) *ProfileService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ProfileService{db: db, resolver: resolver, logger: logger}
}

// UserCard is the public user card.
type UserCard struct {
	Username  string `json:"username"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
	Signature string `json:"signature"`
}

// PostDTO is one user post with its parsed link preview (null when empty).
type PostDTO struct {
	ID           uint         `json:"id"`
	MediaType    string       `json:"media_type"`
	Content      string       `json:"content"`
	MediaURL     string       `json:"media_url"`
	BilibiliBVID string       `json:"bilibili_bvid"`
	LinkPreview  *LinkPreview `json:"link_preview"`
	CreatedAt    int64        `json:"created_at"`
}

// PublicProfile is a user card plus the user's posts, newest first.
type PublicProfile struct {
	User  UserCard  `json:"user"`
	Posts []PostDTO `json:"posts"`
}

// UpdateProfileInput has pointer fields so callers can distinguish "absent"
// (nil, leave unchanged) from "present" (non-nil, apply even when empty).
type UpdateProfileInput struct {
	Nickname  *string `json:"nickname"`
	Signature *string `json:"signature"`
	AvatarURL *string `json:"avatar_url"`
}

// CreatePostInput carries the new-post fields plus an optional
// caller-supplied link preview to persist.
type CreatePostInput struct {
	MediaType    string       `json:"media_type"`
	Content      string       `json:"content"`
	MediaURL     string       `json:"media_url"`
	BilibiliBVID string       `json:"bilibili_bvid"`
	LinkPreview  *LinkPreview `json:"link_preview"`
}

// PostCard is the jump-API card: clients open LinkPreview.TargetURL directly.
type PostCard struct {
	PostID      uint         `json:"post_id"`
	MediaType   string       `json:"media_type"`
	LinkPreview *LinkPreview `json:"link_preview"`
}

var biliBVIDRe = regexp.MustCompile(`^BV[a-zA-Z0-9]{10}$`)

// BilibiliVideoURL builds the canonical watch URL for a BVID.
func BilibiliVideoURL(bvid string) string {
	return "https://www.bilibili.com/video/" + bvid
}

func runeLen(s string) int { return len([]rune(s)) }

func isHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return true
	default:
		return false
	}
}

func parseLinkPreview(raw string) *LinkPreview {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var lp LinkPreview
	if err := json.Unmarshal([]byte(raw), &lp); err != nil {
		return nil
	}
	return &lp
}

func marshalLinkPreview(lp *LinkPreview) string {
	if lp == nil {
		return ""
	}
	b, err := json.Marshal(lp)
	if err != nil {
		return ""
	}
	return string(b)
}

func userCardOf(u *store.User) UserCard {
	return UserCard{
		Username:  u.Username,
		Nickname:  u.Nickname,
		AvatarURL: u.AvatarURL,
		Signature: u.Signature,
	}
}

func postDTOOf(p *store.UserPost) PostDTO {
	return PostDTO{
		ID:           p.ID,
		MediaType:    p.MediaType,
		Content:      p.Content,
		MediaURL:     p.MediaURL,
		BilibiliBVID: p.BilibiliBVID,
		LinkPreview:  parseLinkPreview(p.LinkPreviewJSON),
		CreatedAt:    p.CreatedAt.UnixMilli(),
	}
}

// GetPublicProfile returns the user card plus all non-deleted posts of the
// user, newest first (created_at DESC, id DESC). Unknown or soft-deleted
// users map to 20001.
func (s *ProfileService) GetPublicProfile(username string) (*PublicProfile, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, NewProfileError(CodeParamInvalid, "username is required")
	}
	var u store.User
	if err := s.db.Where("username = ?", username).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, NewProfileError(CodeUserNotFound, "user not found")
		}
		return nil, NewProfileError(CodeInternal, "failed to load user")
	}
	var rows []store.UserPost
	if err := s.db.Where("user_id = ?", u.ID).
		Order("created_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		return nil, NewProfileError(CodeInternal, "failed to load posts")
	}
	posts := make([]PostDTO, 0, len(rows))
	for i := range rows {
		posts = append(posts, postDTOOf(&rows[i]))
	}
	pp := &PublicProfile{User: userCardOf(&u), Posts: posts}
	return pp, nil
}

// UpdateProfile applies a partial update: only non-nil fields are written,
// and an empty string is a legitimate value (it clears the field).
func (s *ProfileService) UpdateProfile(userID string, in UpdateProfileInput) (*UserCard, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, NewProfileError(CodeParamInvalid, "missing user_id")
	}
	if in.Nickname != nil && runeLen(*in.Nickname) > 64 {
		return nil, NewProfileError(CodeParamInvalid, "nickname too long (max 64)")
	}
	if in.Signature != nil && runeLen(*in.Signature) > 255 {
		return nil, NewProfileError(CodeParamInvalid, "signature too long (max 255)")
	}
	if in.AvatarURL != nil && runeLen(*in.AvatarURL) > 255 {
		return nil, NewProfileError(CodeParamInvalid, "avatar_url too long (max 255)")
	}
	var u store.User
	if err := s.db.Where("id = ?", userID).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, NewProfileError(CodeUserNotFound, "user not found")
		}
		return nil, NewProfileError(CodeInternal, "failed to load user")
	}
	updates := map[string]interface{}{}
	if in.Nickname != nil {
		updates["nickname"] = *in.Nickname
		u.Nickname = *in.Nickname
	}
	if in.Signature != nil {
		updates["signature"] = *in.Signature
		u.Signature = *in.Signature
	}
	if in.AvatarURL != nil {
		updates["avatar_url"] = *in.AvatarURL
		u.AvatarURL = *in.AvatarURL
	}
	if len(updates) > 0 {
		if err := s.db.Model(&store.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
			return nil, NewProfileError(CodeInternal, "failed to update profile")
		}
	}
	card := userCardOf(&u)
	return &card, nil
}

// CreatePost validates per the media-type matrix, resolves/persists the link
// preview reservation, and stores the post.
func (s *ProfileService) CreatePost(userID string, in CreatePostInput) (*PostDTO, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, NewProfileError(CodeParamInvalid, "missing user_id")
	}
	switch in.MediaType {
	case "text", "image", "video", "bilibili":
	default:
		return nil, NewProfileError(CodeParamInvalid, "invalid media_type")
	}
	if runeLen(in.Content) > 2000 {
		return nil, NewProfileError(CodeParamInvalid, "content too long (max 2000)")
	}
	switch in.MediaType {
	case "text":
		if strings.TrimSpace(in.Content) == "" {
			return nil, NewProfileError(CodeParamInvalid, "content is required for text posts")
		}
		if strings.TrimSpace(in.MediaURL) != "" {
			return nil, NewProfileError(CodeParamInvalid, "media_url must be empty for text posts")
		}
	case "image", "video":
		if strings.TrimSpace(in.MediaURL) == "" {
			return nil, NewProfileError(CodeParamInvalid, "media_url is required")
		}
	case "bilibili":
		if !biliBVIDRe.MatchString(strings.TrimSpace(in.BilibiliBVID)) {
			return nil, NewProfileError(CodeBilibiliInvalid, "invalid bilibili bvid")
		}
	}
	var u store.User
	if err := s.db.Where("id = ?", userID).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, NewProfileError(CodeUserNotFound, "user not found")
		}
		return nil, NewProfileError(CodeInternal, "failed to load user")
	}

	previewJSON := ""
	if in.LinkPreview != nil {
		if !isHTTPURL(in.LinkPreview.TargetURL) {
			return nil, NewProfileError(CodeParamInvalid, "link_preview.target_url must be http/https")
		}
		previewJSON = marshalLinkPreview(in.LinkPreview)
	} else if s.resolver != nil && in.MediaType == "bilibili" {
		rawURL := BilibiliVideoURL(strings.TrimSpace(in.BilibiliBVID))
		if resolved, err := s.resolver.Resolve(context.Background(), rawURL); err != nil {
			s.logger.Warn("profile: link preview resolve failed", zap.Error(err), zap.String("url", rawURL))
		} else if resolved != nil {
			previewJSON = marshalLinkPreview(resolved)
		}
	}

	row := store.UserPost{
		UserID:          userID,
		MediaType:       in.MediaType,
		Content:         in.Content,
		MediaURL:        in.MediaURL,
		BilibiliBVID:    strings.TrimSpace(in.BilibiliBVID),
		LinkPreviewJSON: previewJSON,
	}
	if err := s.db.Create(&row).Error; err != nil {
		return nil, NewProfileError(CodeInternal, "failed to create post")
	}
	dto := postDTOOf(&row)
	return &dto, nil
}

// DeletePost soft-deletes a post. Only the owner may delete: foreign posts
// map to 10003, missing or already-deleted posts to 40004.
func (s *ProfileService) DeletePost(userID string, postID uint) error {
	if strings.TrimSpace(userID) == "" {
		return NewProfileError(CodeParamInvalid, "missing user_id")
	}
	if postID == 0 {
		return NewProfileError(CodePostNotFound, "post not found")
	}
	var p store.UserPost
	if err := s.db.Where("id = ?", postID).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return NewProfileError(CodePostNotFound, "post not found")
		}
		return NewProfileError(CodeInternal, "failed to load post")
	}
	if p.UserID != userID {
		return NewProfileError(CodeForbidden, "not the post owner")
	}
	if err := s.db.Delete(&p).Error; err != nil {
		return NewProfileError(CodeInternal, "failed to delete post")
	}
	return nil
}

// GetPostCard returns the jump-API card for a post. Stored previews win;
// a bilibili post without a stored preview synthesizes its jump target so
// clients can always open link_preview.target_url directly.
func (s *ProfileService) GetPostCard(postID uint) (*PostCard, error) {
	if postID == 0 {
		return nil, NewProfileError(CodePostNotFound, "post not found")
	}
	var p store.UserPost
	if err := s.db.Where("id = ?", postID).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, NewProfileError(CodePostNotFound, "post not found")
		}
		return nil, NewProfileError(CodeInternal, "failed to load post")
	}
	lp := parseLinkPreview(p.LinkPreviewJSON)
	if lp == nil && p.MediaType == "bilibili" && strings.TrimSpace(p.BilibiliBVID) != "" {
		lp = &LinkPreview{
			Site:      "bilibili",
			TargetURL: BilibiliVideoURL(strings.TrimSpace(p.BilibiliBVID)),
		}
	}
	return &PostCard{PostID: p.ID, MediaType: p.MediaType, LinkPreview: lp}, nil
}
