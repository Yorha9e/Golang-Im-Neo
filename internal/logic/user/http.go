// HTTP adapter for the user-discovery service (thin gin layer only).
//
// Mounted at /api/v1/users with jwtMW:
//
//	GET /?page=...&limit=...&keyword=...
//
// Envelope: {"code":0,"msg":"success","data":{total,page,limit,users}};
// business errors carry SSOT codes with proper (non-200) HTTP status.
package user

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func ok(c *gin.Context, data interface{}) {
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

func handleListUsers(svc *UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me := c.GetString("user_id")
		if me == "" {
			fail(c, NewUserError(CodeUnauthorized, "missing auth context"))
			return
		}
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
		keyword := c.Query("keyword")
		res, err := svc.ListUsers(page, limit, keyword)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, res)
	}
}

// RegisterRoutes mounts the user-discovery endpoints on rg (expected:
// /api/v1/users). jwtMW must populate the user_id context key.
func RegisterRoutes(rg *gin.RouterGroup, svc *UserService, jwtMW gin.HandlerFunc) {
	rg.GET("", jwtMW, handleListUsers(svc))
	rg.GET("/", jwtMW, handleListUsers(svc))
}
