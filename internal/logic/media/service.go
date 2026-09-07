// Package media implements the resource & media layer (mission M2).
//
// SSOT error codes (§3): oversize → 40001, disallowed format → 40002,
// missing param/file → 10001, unauthorized → 10002, forbidden → 10003,
// media not found → 20001, internal → 50001. Envelope {code,msg,data};
// non-zero codes use non-200 HTTP status via HTTPStatusOf.
package media

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/config"
	"golang-im-neo-system/internal/store"
	"golang-im-neo-system/pkg/util"
)

// SSOT error codes for the media layer.
const (
	CodeSuccess           = 0
	CodeParamInvalid      = 10001 // ERR_PARAM_INVALID
	CodeUnauthorized      = 10002 // ERR_UNAUTHORIZED
	CodeForbidden         = 10003 // ERR_FORBIDDEN
	CodeMediaNotFound     = 20001 // ERR_MEDIA_NOT_FOUND
	CodeMediaTooLarge     = 40001 // ERR_MEDIA_TOO_LARGE
	CodeMediaTypeNotAllow = 40002 // ERR_MEDIA_TYPE_NOT_ALLOWED
	CodeInternalServer    = 50001 // ERR_INTERNAL_SERVER
)

// MediaError is a typed business error carrying an SSOT code.
type MediaError struct {
	Code int
	Msg  string
}

func (e *MediaError) Error() string { return e.Msg }

// NewMediaError builds a typed SSOT error.
func NewMediaError(code int, msg string) *MediaError { return &MediaError{Code: code, Msg: msg} }

// CodeOf unwraps the SSOT code from err; unknown errors map to 50001.
func CodeOf(err error) int {
	var me *MediaError
	if errors.As(err, &me) {
		return me.Code
	}
	return CodeInternalServer
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
	case CodeMediaNotFound:
		return 404
	case CodeMediaTooLarge:
		return 400
	case CodeMediaTypeNotAllow:
		return 400
	case CodeInternalServer:
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

// MediaService is the pure media business logic (no gin imports here).
type MediaService struct {
	db     *gorm.DB
	cfg    *config.Config
	logger *zap.Logger
}

// NewMediaService builds the service. cfg carries the [media] block;
// missing values fall back to config defaults so old configs still load.
func NewMediaService(db *gorm.DB, cfg *config.Config, logger *zap.Logger) *MediaService {
	return &MediaService{db: db, cfg: cfg, logger: logger}
}

func (s *MediaService) storageDir() string {
	if s.cfg != nil && strings.TrimSpace(s.cfg.Media.StorageDir) != "" {
		return s.cfg.Media.StorageDir
	}
	return config.DefaultMediaStorageDir
}

func (s *MediaService) maxAvatarBytes() int64 {
	if s.cfg != nil && s.cfg.Media.MaxAvatarBytes > 0 {
		return s.cfg.Media.MaxAvatarBytes
	}
	return config.DefaultMaxAvatarBytes
}

func (s *MediaService) maxFileBytes() int64 {
	if s.cfg != nil && s.cfg.Media.MaxFileBytes > 0 {
		return s.cfg.Media.MaxFileBytes
	}
	return config.DefaultMaxFileBytes
}

func (s *MediaService) avatarExtList() string {
	if s.cfg != nil && strings.TrimSpace(s.cfg.Media.AllowedAvatarExts) != "" {
		return s.cfg.Media.AllowedAvatarExts
	}
	return config.DefaultAllowedAvatarExts
}

func (s *MediaService) fileExtList() string {
	if s.cfg != nil && strings.TrimSpace(s.cfg.Media.AllowedFileExts) != "" {
		return s.cfg.Media.AllowedFileExts
	}
	return config.DefaultAllowedFileExts
}

// parseExtList normalizes a comma-separated ext list into a lookup set.
// Entries are lower-cased and dot-prefixed (bare "png" becomes ".png").
func parseExtList(list string) map[string]bool {
	set := make(map[string]bool)
	for _, tok := range strings.Split(list, ",") {
		e := strings.ToLower(strings.TrimSpace(tok))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		set[e] = true
	}
	return set
}

// Upload stores an uploaded file and returns its MediaAsset row.
//
//   - accessLevel ∈ {public,private}, default private when empty.
//   - mediaType ∈ {avatar,image,voice,video}.
//   - avatar: size ≤ max_avatar_bytes (2MB) and ext in avatar whitelist.
//   - non-avatar: size ≤ max_file_bytes and ext in allowed_file_exts.
//   - The stream is read once while computing sha256 hex.
//   - Dedup: same file_sha256 AND same uploader_id returns the existing
//     asset without writing a duplicate file/row.
//   - Bytes land at filepath.Join(storageDir, mid+ext); AccessURL is
//     "/api/v1/media/"+mid.
func (s *MediaService) Upload(uploaderID, mediaType, accessLevel string, fh *multipart.FileHeader) (*store.MediaAsset, error) {
	uploaderID = strings.TrimSpace(uploaderID)
	if uploaderID == "" {
		return nil, NewMediaError(CodeUnauthorized, "unauthorized: missing uploader")
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	switch mediaType {
	case "avatar", "image", "voice", "video":
	default:
		if mediaType == "" {
			return nil, NewMediaError(CodeParamInvalid, "media_type is required")
		}
		return nil, NewMediaError(CodeMediaTypeNotAllow, "unsupported media_type: "+mediaType)
	}
	accessLevel = strings.ToLower(strings.TrimSpace(accessLevel))
	if accessLevel == "" {
		accessLevel = "private"
	}
	if accessLevel != "public" && accessLevel != "private" {
		return nil, NewMediaError(CodeParamInvalid, "access_level must be public or private")
	}
	if fh == nil || strings.TrimSpace(fh.Filename) == "" {
		return nil, NewMediaError(CodeParamInvalid, "file is required")
	}
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	if ext == "" {
		return nil, NewMediaError(CodeMediaTypeNotAllow, "file extension not allowed")
	}

	var allowed map[string]bool
	var limit int64
	if mediaType == "avatar" {
		allowed = parseExtList(s.avatarExtList())
		limit = s.maxAvatarBytes()
	} else {
		allowed = parseExtList(s.fileExtList())
		limit = s.maxFileBytes()
	}
	if !allowed[ext] {
		return nil, NewMediaError(CodeMediaTypeNotAllow, "file type not allowed: "+ext)
	}
	if fh.Size > 0 && fh.Size > limit {
		return nil, NewMediaError(CodeMediaTooLarge, fmt.Sprintf("file too large: %d bytes exceeds limit %d", fh.Size, limit))
	}

	src, err := fh.Open()
	if err != nil {
		return nil, NewMediaError(CodeInternalServer, "failed to open uploaded file")
	}
	defer src.Close()

	// Single-pass bounded read: cap at limit+1 so an unsized/chunked
	// stream (fh.Size == 0, missing Content-Length) cannot exhaust memory.
	// Oversize is rejected below; the hash covers only accepted bytes.
	h := sha256.New()
	data, err := io.ReadAll(io.TeeReader(io.LimitReader(src, limit+1), h))
	if err != nil {
		return nil, NewMediaError(CodeInternalServer, "failed to read uploaded file")
	}
	if len(data) == 0 {
		return nil, NewMediaError(CodeParamInvalid, "empty file")
	}
	if int64(len(data)) > limit {
		return nil, NewMediaError(CodeMediaTooLarge, fmt.Sprintf("file too large: %d bytes exceeds limit %d", len(data), limit))
	}
	shaHex := hex.EncodeToString(h.Sum(nil))

	// Dedup/秒传: same bytes from the same uploader reuses the first asset.
	var existing store.MediaAsset
	if err := s.db.Where("file_sha256 = ? AND uploader_id = ?", shaHex, uploaderID).First(&existing).Error; err == nil {
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewMediaError(CodeInternalServer, "failed to check duplicate media")
	}

	mid := util.NewMediaID()
	dir := s.storageDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, NewMediaError(CodeInternalServer, "failed to prepare storage dir")
	}
	dst := filepath.Join(dir, mid+ext)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return nil, NewMediaError(CodeInternalServer, "failed to store media file")
	}
	asset := &store.MediaAsset{
		MID:         mid,
		UploaderID:  uploaderID,
		AccessLevel: accessLevel,
		MediaType:   mediaType,
		FileSize:    int64(len(data)),
		FileExt:     ext,
		FileSHA256:  shaHex,
		StoragePath: dst,
		AccessURL:   "/api/v1/media/" + mid,
	}
	if err := s.db.Create(asset).Error; err != nil {
		_ = os.Remove(dst)
		return nil, NewMediaError(CodeInternalServer, "failed to save media asset")
	}
	if s.logger != nil {
		s.logger.Info("media upload", zap.String("mid", mid), zap.String("uploader", uploaderID), zap.String("type", mediaType))
	}
	return asset, nil
}

// Get loads a MediaAsset by mid; missing rows map to 20001.
func (s *MediaService) Get(mid string) (*store.MediaAsset, error) {
	mid = strings.TrimSpace(mid)
	if mid == "" {
		return nil, NewMediaError(CodeParamInvalid, "mid is required")
	}
	var a store.MediaAsset
	// NOTE: struct query so GORM resolves the MID column (m_id) itself.
	if err := s.db.Where(&store.MediaAsset{MID: mid}).First(&a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, NewMediaError(CodeMediaNotFound, "media not found")
		}
		return nil, NewMediaError(CodeInternalServer, "failed to load media")
	}
	return &a, nil
}

// ResolvePath returns the absolute on-disk path for an asset after verifying
// it stays inside storageDir (path-traversal guard). Client-supplied paths
// are never trusted — only the stored StoragePath of a DB-loaded row.
func (s *MediaService) ResolvePath(a *store.MediaAsset) (string, error) {
	if a == nil || strings.TrimSpace(a.StoragePath) == "" {
		return "", NewMediaError(CodeInternalServer, "invalid storage path")
	}
	base, err := filepath.Abs(s.storageDir())
	if err != nil {
		return "", NewMediaError(CodeInternalServer, "invalid storage dir")
	}
	target, err := filepath.Abs(a.StoragePath)
	if err != nil {
		return "", NewMediaError(CodeInternalServer, "invalid storage path")
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", NewMediaError(CodeInternalServer, "invalid storage path")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", NewMediaError(CodeInternalServer, "invalid storage path")
	}
	return target, nil
}
