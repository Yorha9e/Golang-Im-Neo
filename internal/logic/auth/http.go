// HTTP adapter for the auth service (thin gin layer only).
//
// SSOT response fields (TECH_SELECTION_AND_CONTRACTS.md §2.1):
// register → {user_id, username}
// login    → {access_token, refresh_token, user_id, session_id}
// refresh  → {access_token, refresh_token}
// ticket   → {ticket}
// Envelope: {"code":0,"msg":"success","data":{...}}; business errors carry
// SSOT codes with proper (non-200) HTTP status via HTTPStatusOf.
package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type registerReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginReq struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DeviceClass string `json:"device_class"`
	DeviceName  string `json:"device_name"`
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token"`
	SessionID    string `json:"session_id"`
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

func handleRegister(svc *AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req registerReq
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, NewAuthError(CodeParamInvalid, "invalid request body"))
			return
		}
		id, err := svc.Register(req.Username, req.Password)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"user_id": id, "username": strings.TrimSpace(req.Username)})
	}
}

func handleLogin(svc *AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req loginReq
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, NewAuthError(CodeParamInvalid, "invalid request body"))
			return
		}
		if strings.TrimSpace(req.Username) == "" || req.Password == "" {
			fail(c, NewAuthError(CodeParamInvalid, "username and password are required"))
			return
		}
		at, rt, uid, sid, err := svc.Login(req.Username, req.Password, req.DeviceClass, req.DeviceName)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{
			"access_token":  at,
			"refresh_token": rt,
			"user_id":       uid,
			"session_id":    sid,
		})
	}
}

func handleRefresh(svc *AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req refreshReq
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, NewAuthError(CodeParamInvalid, "invalid request body"))
			return
		}
		if req.RefreshToken == "" || req.SessionID == "" {
			fail(c, NewAuthError(CodeParamInvalid, "refresh_token and session_id are required"))
			return
		}
		at, rt, err := svc.Refresh(req.RefreshToken, req.SessionID)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"access_token": at, "refresh_token": rt})
	}
}

func handleTicket(tickets *TicketService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		if userID == "" {
			fail(c, NewAuthError(CodeUnauthorized, "missing auth context"))
			return
		}
		ticket := tickets.Issue(
			userID,
			c.GetString("username"),
			c.GetString("role"),
			c.GetString("session_id"),
			c.GetString("device_class"),
		)
		ok(c, gin.H{"ticket": ticket})
	}
}

// RegisterRoutes mounts the auth endpoints on rg.
// /register, /login, /refresh are public; /ticket is guarded by jwtMW,
// which must have populated the user_id/username/role/session_id/
// device_class context keys.
func RegisterRoutes(rg *gin.RouterGroup, svc *AuthService, tickets *TicketService, jwtMW gin.HandlerFunc) {
	rg.POST("/register", handleRegister(svc))
	rg.POST("/login", handleLogin(svc))
	rg.POST("/refresh", handleRefresh(svc))
	rg.POST("/ticket", jwtMW, handleTicket(tickets))
}
