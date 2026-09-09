// HTTP adapter for the group service (thin gin layer only).
//
// Envelope: {"code":0,"msg":"success","data":{...}}; business errors carry
// stage-4 codes with proper (non-200) HTTP status via HTTPStatusOf — the same
// envelope shape as the friend adapter.
package group

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"golang-im-neo-system/internal/store"
)

type createGroupReq struct {
	Name string `json:"name"`
}

type updateGroupReq struct {
	Name         *string `json:"name"`
	Announcement *string `json:"announcement"`
	AvatarURL    *string `json:"avatar_url"`
}

type inviteReq struct {
	UserIDs []string `json:"user_ids"`
}

type roleReq struct {
	Role string `json:"role"`
}

type muteReq struct {
	Muted *bool `json:"muted"`
}

// historyMsg is one chat record in the history window (ascending seq order).
type historyMsg struct {
	Seq          int64  `json:"seq"`
	FromUID      string `json:"from_uid"`
	FromUsername string `json:"from_username,omitempty"`
	FromAvatar   string `json:"from_avatar,omitempty"`
	FromRole     string `json:"from_role,omitempty"`
	Content      string `json:"content"`
	Extra        string `json:"extra"`
	Timestamp    int64  `json:"timestamp"`
	StanzaID     string `json:"stanza_id"`
	ContentType  int8   `json:"content_type"`
}

func ok(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": data})
}

func created(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, gin.H{"code": 0, "msg": "success", "data": data})
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
		fail(c, NewGroupError(CodeUnauthorized, "missing auth context"))
		return "", false
	}
	return me, true
}

// requireMembership gates member-only reads with 40002.
func requireMembership(c *gin.Context, svc *GroupService, groupID, userID string) bool {
	if _, err := svc.GetMember(groupID, userID); err != nil {
		fail(c, err)
		return false
	}
	return true
}

func handleCreate(svc *GroupService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		var req createGroupReq
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, NewGroupError(CodeParamInvalid, "invalid request body"))
			return
		}
		v, err := svc.CreateGroup(me, req.Name)
		if err != nil {
			fail(c, err)
			return
		}
		created(c, v)
	}
}

func handleListMine(svc *GroupService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		groups, err := svc.ListMyGroups(me)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, groups)
	}
}

func handleGet(svc *GroupService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		id := c.Param("id")
		if !requireMembership(c, svc, id, me) {
			return
		}
		v, err := svc.GetGroup(id)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, v)
	}
}

func handleUpdate(svc *GroupService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		id := c.Param("id")
		var req updateGroupReq
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, NewGroupError(CodeParamInvalid, "invalid request body"))
			return
		}
		if err := svc.UpdateGroupInfo(me, id, req.Name, req.Announcement, req.AvatarURL); err != nil {
			fail(c, err)
			return
		}
		v, err := svc.GetGroup(id)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, v)
	}
}

func handleInvite(svc *GroupService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		id := c.Param("id")
		var req inviteReq
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, NewGroupError(CodeParamInvalid, "invalid request body"))
			return
		}
		invited, err := svc.Invite(me, id, req.UserIDs)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"invited": invited})
	}
}

func handleJoin(svc *GroupService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		id := c.Param("id")
		if err := svc.Join(me, id); err != nil {
			fail(c, err)
			return
		}
		v, err := svc.GetGroup(id)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, v)
	}
}

func handleLeave(svc *GroupService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		if err := svc.Leave(me, c.Param("id")); err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"ok": true})
	}
}

func handleDismiss(svc *GroupService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		if err := svc.Dismiss(me, c.Param("id")); err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"ok": true})
	}
}

func handleKick(svc *GroupService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		if err := svc.Kick(me, c.Param("id"), c.Param("uid")); err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"ok": true})
	}
}

func handleSetRole(svc *GroupService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		var req roleReq
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, NewGroupError(CodeParamInvalid, "invalid request body"))
			return
		}
		if err := svc.SetRole(me, c.Param("id"), c.Param("uid"), req.Role); err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"ok": true})
	}
}

func handleSetMute(svc *GroupService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		var req muteReq
		if err := c.ShouldBindJSON(&req); err != nil || req.Muted == nil {
			fail(c, NewGroupError(CodeParamInvalid, "invalid request body"))
			return
		}
		if err := svc.SetMuted(me, c.Param("id"), c.Param("uid"), *req.Muted); err != nil {
			fail(c, err)
			return
		}
		ok(c, gin.H{"ok": true})
	}
}

func handleMembers(svc *GroupService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		id := c.Param("id")
		if !requireMembership(c, svc, id, me) {
			return
		}
		members, err := svc.ListMembers(id)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, members)
	}
}

func handleHistory(svc *GroupService) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, okAuth := actorOf(c)
		if !okAuth {
			return
		}
		id := c.Param("id")
		if !requireMembership(c, svc, id, me) {
			return
		}
		var beforeSeq int64
		if raw := c.Query("before_seq"); raw != "" {
			if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
				beforeSeq = v
			}
		}
		limit := 50
		if raw := c.Query("limit"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				limit = v
			}
		}
		if limit < 1 {
			limit = 1
		}
		if limit > 100 {
			limit = 100
		}
		q := svc.db.Where("cov_id = ?", BuildCovID(id))
		if beforeSeq > 0 {
			q = q.Where("seq < ?", beforeSeq)
		}
		var rows []store.Message
		if err := q.Order("seq DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
			fail(c, NewGroupError(CodeInternal, "failed to load history"))
			return
		}
		hasMore := len(rows) > limit
		if hasMore {
			rows = rows[:limit]
		}
		// Return messages in ASCENDING seq order inside the array.
		seen := make(map[string]struct{}, len(rows))
		uniqueUIDs := make([]string, 0, len(rows))
		for _, m := range rows {
			if m.FromUID == "" {
				continue
			}
			if _, ok := seen[m.FromUID]; !ok {
				seen[m.FromUID] = struct{}{}
				uniqueUIDs = append(uniqueUIDs, m.FromUID)
			}
		}
		userMap := make(map[string]store.User)
		if len(uniqueUIDs) > 0 {
			var users []store.User
			if err := svc.db.Model(&store.User{}).Select("id, username, avatar_url, role").Where("id IN ?", uniqueUIDs).Find(&users).Error; err == nil {
				for _, u := range users {
					userMap[u.ID] = u
				}
			}
		}
		msgs := make([]historyMsg, 0, len(rows))
		for i := len(rows) - 1; i >= 0; i-- {
			m := rows[i]
			hm := historyMsg{
				Seq:         m.Seq,
				FromUID:     m.FromUID,
				Content:     m.Content,
				Extra:       m.Extra,
				Timestamp:   m.Timestamp,
				StanzaID:    m.StanzaID,
				ContentType: m.ContentType,
			}
			if u, ok := userMap[m.FromUID]; ok {
				hm.FromUsername = u.Username
				hm.FromAvatar = u.AvatarURL
				hm.FromRole = u.Role
			}
			msgs = append(msgs, hm)
		}
		ok(c, gin.H{"messages": msgs, "has_more": hasMore})
	}
}

// RegisterRoutes mounts the group endpoints on rg, where rg is the
// /api/v1/groups group. All routes are JWT-guarded via jwtMW, which must
// have populated the user_id context key.
func RegisterRoutes(rg *gin.RouterGroup, svc *GroupService, jwtMW gin.HandlerFunc) {
	rg.POST("", jwtMW, handleCreate(svc))
	rg.POST("/", jwtMW, handleCreate(svc))
	rg.GET("", jwtMW, handleListMine(svc))
	rg.GET("/", jwtMW, handleListMine(svc))
	rg.GET("/:id", jwtMW, handleGet(svc))
	rg.PATCH("/:id", jwtMW, handleUpdate(svc))
	rg.POST("/:id/invite", jwtMW, handleInvite(svc))
	rg.POST("/:id/join", jwtMW, handleJoin(svc))
	rg.POST("/:id/leave", jwtMW, handleLeave(svc))
	rg.POST("/:id/dismiss", jwtMW, handleDismiss(svc))
	rg.DELETE("/:id/members/:uid", jwtMW, handleKick(svc))
	rg.PATCH("/:id/members/:uid/role", jwtMW, handleSetRole(svc))
	rg.PATCH("/:id/members/:uid/mute", jwtMW, handleSetMute(svc))
	rg.GET("/:id/members", jwtMW, handleMembers(svc))
	rg.GET("/:id/history", jwtMW, handleHistory(svc))
}
