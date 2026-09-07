// HTTP adapter for the message-history service (thin gin layer only).
//
// Envelope: {"code":0,"msg":"success","data":{...}}; business errors carry
// SSOT codes with proper (non-200) HTTP status via HTTPStatusOf.
//
// Routes (mounted at /api/v1/messages):
//
//	GET /history?cov_id=...&before_seq=...&limit=...
//	GET /private/:target_user_id/history?before_seq=...&limit=...
//	GET /hall/history?before_seq=...&limit=...
package message

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"golang-im-neo-system/internal/logic/session"
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

func actorOf(c *gin.Context) (string, bool) {
	me := c.GetString("user_id")
	if me == "" {
		fail(c, NewMessageError(CodeUnauthorized, "missing auth context"))
		return "", false
	}
	return me, true
}

// parsePagination extracts before_seq (default 0) and limit (default 50,
// clamped to [1,100]) from the query string. Unparseable or non-positive
// values fall back to the defaults, mirroring the group history endpoint.
func parsePagination(c *gin.Context) (int64, int) {
	var beforeSeq int64
	if raw := strings.TrimSpace(c.Query("before_seq")); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			beforeSeq = v
		}
	}
	limit := DefaultLimit
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	return beforeSeq, limit
}

func handleHistory(svc *MessageService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		covID := strings.TrimSpace(c.Query("cov_id"))
		if covID == "" {
			fail(c, NewMessageError(CodeParamInvalid, "cov_id is required"))
			return
		}
		beforeSeq, limit := parsePagination(c)
		res, err := svc.GetHistory(me, covID, beforeSeq, limit)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"messages": res.Messages, "has_more": res.HasMore})
	}
}

func handlePrivateHistory(svc *MessageService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		target := strings.TrimSpace(c.Param("target_user_id"))
		if target == "" {
			fail(c, NewMessageError(CodeParamInvalid, "target_user_id is required"))
			return
		}
		covID := session.BuildPrivateCovID(me, target)
		beforeSeq, limit := parsePagination(c)
		res, err := svc.GetHistory(me, covID, beforeSeq, limit)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"messages": res.Messages, "has_more": res.HasMore})
	}
}

func handleHallHistory(svc *MessageService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		beforeSeq, limit := parsePagination(c)
		res, err := svc.GetHistory(me, PublicHallCovID, beforeSeq, limit)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"messages": res.Messages, "has_more": res.HasMore})
	}
}

// RegisterRoutes mounts the message-history endpoints on rg, where rg is the
// /api/v1/messages group. All routes are JWT-guarded via jwtMW, which must
// have populated the user_id context key.
func RegisterRoutes(rg *gin.RouterGroup, svc *MessageService, jwtMW gin.HandlerFunc) {
	rg.GET("/history", jwtMW, handleHistory(svc))
	rg.GET("/private/:target_user_id/history", jwtMW, handlePrivateHistory(svc))
	rg.GET("/hall/history", jwtMW, handleHallHistory(svc))
}
