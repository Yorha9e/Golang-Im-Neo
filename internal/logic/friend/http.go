// HTTP adapter for the friend service (thin gin layer only).
//
// Envelope: {"code":0,"msg":"success","data":{...}}; business errors carry
// SSOT codes with proper (non-200) HTTP status via HTTPStatusOf.
package friend

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type applyReq struct {
	TargetUsername string `json:"target_username"`
	Remark         string `json:"remark"`
}

type respondReq struct {
	TargetUserID string `json:"target_user_id"`
	Action       string `json:"action"`
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

func handleList(svc *FriendService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me := c.GetString("user_id")
		if me == "" {
			fail(c, NewFriendError(CodeUnauthorized, "missing auth context"))
			return
		}
		friends, err := svc.ListFriends(me)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"friends": friends})
	}
}

func handlePending(svc *FriendService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me := c.GetString("user_id")
		if me == "" {
			fail(c, NewFriendError(CodeUnauthorized, "missing auth context"))
			return
		}
		reqs, err := svc.ListPending(me)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"requests": reqs})
	}
}

func handleApply(svc *FriendService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me := c.GetString("user_id")
		if me == "" {
			fail(c, NewFriendError(CodeUnauthorized, "missing auth context"))
			return
		}
		var req applyReq
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, NewFriendError(CodeParamInvalid, "invalid request body"))
			return
		}
		targetID, err := svc.Apply(me, req.TargetUsername, req.Remark)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"target_user_id": targetID})
	}
}

func handleRespond(svc *FriendService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me := c.GetString("user_id")
		if me == "" {
			fail(c, NewFriendError(CodeUnauthorized, "missing auth context"))
			return
		}
		var req respondReq
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, NewFriendError(CodeParamInvalid, "invalid request body"))
			return
		}
		if err := svc.Respond(me, req.TargetUserID, req.Action); err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"target_user_id": req.TargetUserID, "action": req.Action})
	}
}

func handleDelete(svc *FriendService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me := c.GetString("user_id")
		if me == "" {
			fail(c, NewFriendError(CodeUnauthorized, "missing auth context"))
			return
		}
		friendID := c.Param("friend_id")
		if err := svc.Delete(me, friendID); err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"friend_id": friendID})
	}
}

// RegisterRoutes mounts the friend endpoints on rg, where rg is the
// /api/v1/friends group. All routes are JWT-guarded via jwtMW, which must
// have populated the user_id context key.
func RegisterRoutes(rg *gin.RouterGroup, svc *FriendService, jwtMW gin.HandlerFunc) {
	rg.GET("", jwtMW, handleList(svc))
	rg.GET("/", jwtMW, handleList(svc))
	rg.GET("/pending", jwtMW, handlePending(svc))
	rg.POST("/apply", jwtMW, handleApply(svc))
	rg.POST("/respond", jwtMW, handleRespond(svc))
	rg.DELETE("/:friend_id", jwtMW, handleDelete(svc))
}
