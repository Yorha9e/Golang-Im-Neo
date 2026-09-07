package router

import (
	"container/list"
	"sync"
	"time"

	"go.uber.org/zap"

	pb "golang-im-neo-system/proto"

	"golang-im-neo-system/internal/logic/group"
	"golang-im-neo-system/internal/logic/session"
	"golang-im-neo-system/internal/store"
)

// ---- Rate limiter (hand-rolled token bucket, no new deps) ----

const (
	// rateLimitPerSec refills 10 tokens/second per user.
	rateLimitPerSec = 10.0
	// rateLimitBurst allows short bursts up to 20 messages.
	rateLimitBurst = 20.0
)

type tokenBucket struct {
	tokens float64
	last   time.Time
}

// rateLimiter holds one token bucket per user. Buckets are created lazily and
// full; steady state is 10 msg/s with a burst headroom of 20. NOTE: entries
// are never expired in stage-1 (bounded by user count); stage-2 may add idle
// eviction if needed.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: make(map[string]*tokenBucket)}
}

func (l *rateLimiter) allowAt(userID string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[userID]
	if !ok {
		b = &tokenBucket{tokens: rateLimitBurst, last: now}
		l.buckets[userID] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * rateLimitPerSec
		if b.tokens > rateLimitBurst {
			b.tokens = rateLimitBurst
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *rateLimiter) allow(userID string) bool {
	return l.allowAt(userID, time.Now())
}

// rateLimitInterceptor sheds per-user floods before any state mutation.
// HEARTBEAT frames are exempt (connection keepalive must never be throttled).
// Denied frames are dropped with a SYSTEM_NOTICE warning to the sender.
func (r *Router) rateLimitInterceptor(ctx *MessageContext, next func() error) error {
	t := ctx.Msg.GetType()
	if t == pb.MsgType_HEARTBEAT_PING || t == pb.MsgType_HEARTBEAT_PONG {
		return next()
	}
	if !r.limiter.allow(ctx.UserID) {
		r.logger.Warn("router: rate limited, dropping frame",
			zap.String("user", ctx.UserID), zap.String("type", t.String()))
		sendPlainNoticeTo(r.emitter, r.logger, ctx.UserID, "rate limited, please slow down")
		return nil
	}
	return next()
}

// ---- Security / identity interceptor ----

// securityIdentityInterceptor enforces server-side identity: the wire
// FromUID/Seq/Timestamp are untrusted client input and are ALWAYS overwritten
// here (spoof-proofing; server assigns seq + physical timestamp downstream).
func (r *Router) securityIdentityInterceptor(ctx *MessageContext, next func() error) error {
	ctx.Msg.FromUid = ctx.UserID
	ctx.Msg.Seq = 0
	ctx.Msg.Timestamp = 0
	return next()
}

// ---- Friendship check interceptor (PRIVATE_CHAT only) ----

const (
	// friendCacheCap bounds the LRU (hand-rolled over container/list).
	friendCacheCap = 1024
	// friendCacheTTL bounds staleness of allow/deny verdicts (60s).
	friendCacheTTL = 60 * time.Second
)

type friendEntry struct {
	key     string
	allowed bool
	expires time.Time
}

// friendCache is a hand-rolled TTL-LRU keyed by session.BuildPrivateCovID.
// Both allow AND deny verdicts are cached (deny caching stops DB hammering
// from repeated non-friend spam). Stage-2 friend ops must call
// Router.Invalidate(covKey) on accept/unfriend/block changes.
type friendCache struct {
	mu    sync.Mutex
	ll    *list.List // []*friendEntry, front = most recent
	items map[string]*list.Element
	cap   int
	ttl   time.Duration
}

func newFriendCache(capacity int, ttl time.Duration) *friendCache {
	if capacity <= 0 {
		capacity = friendCacheCap
	}
	if ttl <= 0 {
		ttl = friendCacheTTL
	}
	return &friendCache{
		ll:    list.New(),
		items: make(map[string]*list.Element),
		cap:   capacity,
		ttl:   ttl,
	}
}

// get returns (allowed, true) on a live cache hit, (_, false) on miss/expired.
func (c *friendCache) get(key string) (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return false, false
	}
	e := el.Value.(*friendEntry)
	if time.Now().After(e.expires) {
		c.ll.Remove(el)
		delete(c.items, key)
		return false, false
	}
	c.ll.MoveToFront(el)
	return e.allowed, true
}

func (c *friendCache) add(key string, allowed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		e := el.Value.(*friendEntry)
		e.allowed = allowed
		e.expires = time.Now().Add(c.ttl)
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&friendEntry{key: key, allowed: allowed, expires: time.Now().Add(c.ttl)})
	c.items[key] = el
	for c.ll.Len() > c.cap {
		back := c.ll.Back()
		if back == nil {
			break
		}
		c.ll.Remove(back)
		delete(c.items, back.Value.(*friendEntry).key)
	}
}

func (c *friendCache) invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.ll.Remove(el)
		delete(c.items, key)
	}
}

// Invalidate drops one cached friendship verdict (key = session.BuildPrivateCovID).
// Exported for stage-2 friend apply/accept/unfriend/block flows.
func (r *Router) Invalidate(covKey string) {
	r.friends.invalidate(covKey)
}

// InvalidateGroupMember drops one cached group-membership verdict
// (key grp:<groupID>:<userID>). Exported for future group kick/leave/mute
// flows; mute itself needs no invalidation (checked live every time).
func (r *Router) InvalidateGroupMember(groupID, userID string) {
	r.groups.invalidate(groupMemberKey(groupID, userID))
}

// groupMemberKey is the group membership cache key (mirrors the friendCache
// private-cov-id keying with a distinct grp: namespace).
func groupMemberKey(groupID, userID string) string {
	return "grp:" + groupID + ":" + userID
}

// groupChatInterceptor gates GROUP_CHAT on active membership + live mute.
// Miss path probes group.IsMember (allow AND deny cached 60s); mute is then
// probed LIVE via group.IsMuted on every frame so moderation takes effect
// immediately. Deny => coded SYSTEM_NOTICE (40002 / 40005); NO persist,
// NO push. Non-group frames pass through. DB errors fail closed with 50001
// and are NOT cached.
func (r *Router) groupChatInterceptor(ctx *MessageContext, next func() error) error {
	if ctx.Msg.GetType() != pb.MsgType_GROUP_CHAT {
		return next()
	}
	gid := ctx.Msg.GetToUid()
	if gid == "" {
		return next() // dispatch reports missing-recipient; no DB trip needed
	}
	key := groupMemberKey(gid, ctx.UserID)
	if allowed, ok := r.groups.get(key); ok {
		if !allowed {
			sendCodeNoticeTo(r.emitter, r.logger, ctx.UserID,
				CodeGroupNotMember, "not a group member", "not a group member, message dropped")
			return nil
		}
	} else {
		ok, err := group.IsMember(r.db, gid, ctx.UserID)
		if err != nil {
			r.logger.Error("router: group membership lookup failed",
				zap.String("group", gid), zap.String("from", ctx.UserID), zap.Error(err))
			sendCodeNoticeTo(r.emitter, r.logger, ctx.UserID,
				CodeInternalError, "internal error", "internal error, please retry")
			return nil
		}
		r.groups.add(key, ok)
		if !ok {
			r.logger.Info("router: group chat blocked, not a member",
				zap.String("group", gid), zap.String("from", ctx.UserID))
			sendCodeNoticeTo(r.emitter, r.logger, ctx.UserID,
				CodeGroupNotMember, "not a group member", "not a group member, message dropped")
			return nil
		}
	}
	// Mute is deliberately uncached: a mute/unmute must bite on the next frame.
	muted, err := group.IsMuted(r.db, gid, ctx.UserID)
	if err != nil {
		r.logger.Error("router: group mute lookup failed",
			zap.String("group", gid), zap.String("from", ctx.UserID), zap.Error(err))
		sendCodeNoticeTo(r.emitter, r.logger, ctx.UserID,
			CodeInternalError, "internal error", "internal error, please retry")
		return nil
	}
	if muted {
		r.logger.Info("router: group chat blocked, muted",
			zap.String("group", gid), zap.String("from", ctx.UserID))
		sendCodeNoticeTo(r.emitter, r.logger, ctx.UserID,
			CodeGroupMuted, "muted in group", "muted in group, message dropped")
		return nil
	}
	return next()
}

// friendshipCheckInterceptor gates PRIVATE_CHAT on an accepted friendship row.
// Miss path queries the DB (user_id=from AND friend_id=to AND status='accepted'
// AND deleted_at IS NULL — GORM soft-delete already scopes deleted_at; the
// explicit predicate mirrors the SSOT shape). Deny => SYSTEM_NOTICE with SSOT
// code 30001 to the sender; NO persist, NO push. Non-private frames pass through.
func (r *Router) friendshipCheckInterceptor(ctx *MessageContext, next func() error) error {
	if ctx.Msg.GetType() != pb.MsgType_PRIVATE_CHAT {
		return next()
	}
	to := ctx.Msg.GetToUid()
	if to == "" {
		return next() // dispatch reports missing-recipient; no DB trip needed
	}
	if to == ctx.UserID {
		return next() // self-notes need no friendship
	}
	key := session.BuildPrivateCovID(ctx.UserID, to)
	if allowed, ok := r.friends.get(key); ok {
		if allowed {
			return next()
		}
		sendCodeNoticeTo(r.emitter, r.logger, ctx.UserID,
			CodeFriendNotFound, "friend not found", "not friends, private chat blocked")
		return nil
	}
	var cnt int64
	q := r.db.Model(&store.Friendship{}).
		Where("user_id = ? AND friend_id = ? AND status = 'accepted'", ctx.UserID, to).
		Where("deleted_at IS NULL")
	if err := q.Count(&cnt).Error; err != nil {
		// Fail closed on DB errors (never deliver on uncertain relation),
		// reported as internal error, NOT cached.
		r.logger.Error("router: friendship lookup failed",
			zap.String("from", ctx.UserID), zap.String("to", to), zap.Error(err))
		sendCodeNoticeTo(r.emitter, r.logger, ctx.UserID,
			CodeInternalError, "internal error", "internal error, please retry")
		return nil
	}
	if cnt > 0 {
		r.friends.add(key, true)
		return next()
	}
	r.friends.add(key, false)
	r.logger.Info("router: private chat blocked, not friends",
		zap.String("from", ctx.UserID), zap.String("to", to))
	sendCodeNoticeTo(r.emitter, r.logger, ctx.UserID,
		CodeFriendNotFound, "friend not found", "not friends, private chat blocked")
	return nil
}
