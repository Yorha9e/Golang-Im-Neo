// HTTP adapter for the profile service (thin gin layer only).
//
// Envelope: {"code":0,"msg":"success","data":{...}}; business errors carry
// SSOT codes with proper (non-200) HTTP status via HTTPStatusOf.
//
// Route layout: the public profile card lives at GET /api/v1/profile/:username
// while the post jump-card lives on a separate group at
// GET /api/v1/posts/:post_id/card. gin panics when a static segment and a
// :param collide at the same position within one HTTP-method tree, so the
// card route must NOT be mounted under /api/v1/profile.
package profile

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type updateProfileReq struct {
	Nickname  *string `json:"nickname"`
	Signature *string `json:"signature"`
	AvatarURL *string `json:"avatar_url"`
}

type createPostReq struct {
	MediaType    string       `json:"media_type"`
	Content      string       `json:"content"`
	MediaURL     string       `json:"media_url"`
	BilibiliBVID string       `json:"bilibili_bvid"`
	LinkPreview  *LinkPreview `json:"link_preview"`
}

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

func handleGetPublic(svc *ProfileService) gin.HandlerFunc {
	return func(c *gin.Context) {
		pp, err := svc.GetPublicProfile(c.Param("username"))
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"user": pp.User, "posts": pp.Posts})
	}
}

func handleUpdate(svc *ProfileService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me := c.GetString("user_id")
		if me == "" {
			fail(c, NewProfileError(CodeUnauthorized, "missing auth context"))
			return
		}
		var req updateProfileReq
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, NewProfileError(CodeParamInvalid, "invalid request body"))
			return
		}
		card, err := svc.UpdateProfile(me, UpdateProfileInput{
			Nickname:  req.Nickname,
			Signature: req.Signature,
			AvatarURL: req.AvatarURL,
		})
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"user": card})
	}
}

func handleCreatePost(svc *ProfileService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me := c.GetString("user_id")
		if me == "" {
			fail(c, NewProfileError(CodeUnauthorized, "missing auth context"))
			return
		}
		var req createPostReq
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, NewProfileError(CodeParamInvalid, "invalid request body"))
			return
		}
		dto, err := svc.CreatePost(me, CreatePostInput{
			MediaType:    req.MediaType,
			Content:      req.Content,
			MediaURL:     req.MediaURL,
			BilibiliBVID: req.BilibiliBVID,
			LinkPreview:  req.LinkPreview,
		})
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"post": dto})
	}
}

func parsePostIDParam(c *gin.Context) (uint, error) {
	raw := c.Param("post_id")
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, NewProfileError(CodeParamInvalid, "invalid post_id")
	}
	return uint(n), nil
}

func handleDeletePost(svc *ProfileService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me := c.GetString("user_id")
		if me == "" {
			fail(c, NewProfileError(CodeUnauthorized, "missing auth context"))
			return
		}
		id, err := parsePostIDParam(c)
		if err != nil {
			fail(c, err)
			return
		}
		if err := svc.DeletePost(me, id); err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"post_id": id})
	}
}

func handlePostCard(svc *ProfileService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := parsePostIDParam(c)
		if err != nil {
			fail(c, err)
			return
		}
		card, err := svc.GetPostCard(id)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"post_id": card.PostID, "media_type": card.MediaType, "link_preview": card.LinkPreview})
	}
}

// RegisterRoutes mounts the profile endpoints: profileRG is the
// /api/v1/profile group and postsCardRG is the /api/v1/posts group.
// The card route lives on the posts group to avoid a gin static/:param
// collision with GET /:username. Auth-guarded routes take jwtMW, which must
// have populated the user_id context key; the two GET routes are public.
func RegisterRoutes(profileRG, postsCardRG *gin.RouterGroup, svc *ProfileService, jwtMW gin.HandlerFunc) {
	profileRG.GET("/:username", handleGetPublic(svc))
	profileRG.PUT("", jwtMW, handleUpdate(svc))
	profileRG.PUT("/", jwtMW, handleUpdate(svc))
	profileRG.POST("/posts", jwtMW, handleCreatePost(svc))
	profileRG.DELETE("/posts/:post_id", jwtMW, handleDeletePost(svc))
	postsCardRG.GET("/:post_id/card", handlePostCard(svc))
}
