// HTTP adapter for the admin service (thin gin layer only).
//
// Mounted at /api/v1/admin with jwtMW then adminMW chained on every route:
//
//	POST /users/:user_id/ban
//	POST /sessions/:session_id/kick
//	POST /broadcast  (body {"content":"..."}; "message" accepted as alias)
//	GET /stats
//	GET /users?page=...&limit=...&status=...&keyword=...
//
// Envelope: {"code":0,"msg":"success","data":{...}}; business errors carry
// SSOT codes with proper (non-200) HTTP status via HTTPStatusOf.
package admin

import (
	"net/http"
	"strconv"
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

type broadcastReq struct {
	Content string `json:"content"`
	Message string `json:"message"` // alias for Content
}

func handleBanUser(svc *AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("user_id")
		if userID == "" {
			fail(c, NewAdminError(CodeParamInvalid, "user_id is required"))
			return
		}
		if err := svc.BanUser(userID); err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"user_id": userID})
	}
}

func handleKickSession(svc *AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("session_id")
		if sessionID == "" {
			fail(c, NewAdminError(CodeParamInvalid, "session_id is required"))
			return
		}
		if err := svc.KickSession(sessionID); err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"session_id": sessionID})
	}
}

func handleBroadcast(svc *AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req broadcastReq
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, NewAdminError(CodeParamInvalid, "invalid request body"))
			return
		}
		content := req.Content
		if content == "" {
			content = req.Message
		}
		if err := svc.Broadcast(content); err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"content": content})
	}
}

func handleGetStats(svc *AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		stats, err := svc.GetStats()
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": stats})
	}
}

func handleListUsers(svc *AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := 0
		if raw := strings.TrimSpace(c.Query("page")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil {
				page = v
			}
		}
		limit := 0
		if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil {
				limit = v
			}
		}
		var status *int8
		if raw := strings.TrimSpace(c.Query("status")); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil {
				fail(c, NewAdminError(CodeParamInvalid, "invalid status"))
				return
			}
			sv := int8(v)
			status = &sv
		}
		keyword := c.Query("keyword")
		res, err := svc.ListUsers(page, limit, status, keyword)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": res})
	}
}

// RegisterRoutes mounts the admin endpoints on rg (expected: /api/v1/admin).
// jwtMW must populate the role context key; adminMW must enforce admin-only.
func RegisterRoutes(rg *gin.RouterGroup, svc *AdminService, jwtMW, adminMW gin.HandlerFunc) {
	rg.POST("/users/:user_id/ban", jwtMW, adminMW, handleBanUser(svc))
	rg.POST("/sessions/:session_id/kick", jwtMW, adminMW, handleKickSession(svc))
	rg.POST("/broadcast", jwtMW, adminMW, handleBroadcast(svc))
	rg.GET("/stats", jwtMW, adminMW, handleGetStats(svc))
	rg.GET("/users", jwtMW, adminMW, handleListUsers(svc))
	rg.GET("/users/", jwtMW, adminMW, handleListUsers(svc))
}
