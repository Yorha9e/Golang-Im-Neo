// HTTP adapter for the media service (thin gin layer only).
//
// POST /upload is JWT-guarded. GET /:mid enforces conditional auth inside
// the handler: public assets stream anonymously; private assets require a
// valid JWT and uploader ownership (anonymous → 401, non-uploader → 403).
// Envelope: {"code":0,"msg":"success","data":{...}}; business errors carry
// SSOT codes with proper (non-200) HTTP status via HTTPStatusOf.
package media

import (
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

func ok(c *gin.Context, data gin.H) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": data})
}

func fail(c *gin.Context, err error) {
	code := CodeOf(err)
	msg := err.Error()
	if msg == "" {
		msg = "error"
	}
	c.JSON(HTTPStatusOf(code), gin.H{"code": code, "msg": msg})
}

func handleUpload(svc *MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.GetString("user_id")
		if uid == "" {
			fail(c, NewMediaError(CodeUnauthorized, "unauthorized"))
			return
		}
		fh, err := c.FormFile("file")
		if err != nil || fh == nil {
			fail(c, NewMediaError(CodeParamInvalid, "file is required"))
			return
		}
		asset, err := svc.Upload(uid, c.PostForm("media_type"), c.PostForm("access_level"), fh)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{
			"mid":          asset.MID,
			"access_url":   asset.AccessURL,
			"media_type":   asset.MediaType,
			"access_level": asset.AccessLevel,
			"file_size":    asset.FileSize,
			"file_ext":     asset.FileExt,
			"sha256":       asset.FileSHA256,
		})
	}
}

func handleGet(svc *MediaService, jwtMW gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		asset, err := svc.Get(c.Param("mid"))
		if err != nil {
			fail(c, err)
			return
		}
		// Access isolation: public streams anonymously; private requires
		// a valid JWT and uploader ownership.
		if asset.AccessLevel != "public" {
			if jwtMW == nil {
				fail(c, NewMediaError(CodeUnauthorized, "unauthorized"))
				return
			}
			jwtMW(c)
			if c.IsAborted() {
				return
			}
			uid := c.GetString("user_id")
			if uid == "" {
				fail(c, NewMediaError(CodeUnauthorized, "unauthorized"))
				return
			}
			if uid != asset.UploaderID {
				fail(c, NewMediaError(CodeForbidden, "forbidden: only uploader may access"))
				return
			}
		}
		p, err := svc.ResolvePath(asset)
		if err != nil {
			fail(c, err)
			return
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			fail(c, NewMediaError(CodeInternalServer, "media file missing"))
			return
		}
		c.Header("Content-Type", contentTypeForExt(asset.FileExt))
		c.File(p)
	}
}

// RegisterRoutes mounts the media endpoints on rg (expected /api/v1/media).
// POST /upload is guarded by jwtMW; GET /:mid enforces conditional auth
// inside the handler so public assets stay anonymously readable.
func RegisterRoutes(rg *gin.RouterGroup, svc *MediaService, jwtMW gin.HandlerFunc) {
	rg.POST("/upload", jwtMW, handleUpload(svc))
	rg.GET("/:mid", handleGet(svc, jwtMW))
}

// contentTypeForExt maps a stored file ext to its stream Content-Type.
func contentTypeForExt(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".mp3":
		return "audio/mpeg"
	case ".aac":
		return "audio/aac"
	case ".wav":
		return "audio/wav"
	case ".amr":
		return "audio/amr"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
