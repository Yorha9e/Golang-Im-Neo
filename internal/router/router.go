package router

import (
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	pb "golang-im-neo-system/proto"

	"golang-im-neo-system/internal/logic/session"
)

// Router is the inbound dispatcher. M3's gateway readPump calls exactly
// HandleInbound on every received binary frame; this runs the interceptor
// chain (rate limit -> identity -> friendship) and dispatches by MsgType.
type Router struct {
	emitter      Emitter
	persister    Persister
	alloc        *session.Allocator
	db           *gorm.DB
	logger       *zap.Logger
	dedup        *DedupWindows
	limiter      *rateLimiter
	friends      *friendCache
	strategy     RelayStrategy
	interceptors []Interceptor
}

// New builds a Router. emitter/persister/alloc/db must be non-nil (production
// wiring owns them); a nil logger becomes zap.NewNop(). Chain order follows
// SSOT §3.2: RateLimit -> SecurityIdentity -> FriendshipCheck.
func New(emitter Emitter, persister Persister, alloc *session.Allocator, db *gorm.DB, logger *zap.Logger) *Router {
	if logger == nil {
		logger = zap.NewNop()
	}
	r := &Router{
		emitter:   emitter,
		persister: persister,
		alloc:     alloc,
		db:        db,
		logger:    logger,
		dedup:     NewDedupWindows(),
		limiter:   newRateLimiter(),
		friends:   newFriendCache(friendCacheCap, friendCacheTTL),
	}
	r.strategy = NewCentralRelayStrategy(emitter, persister, alloc, r.dedup, logger)
	r.interceptors = []Interceptor{
		r.rateLimitInterceptor,
		r.securityIdentityInterceptor,
		r.friendshipCheckInterceptor,
	}
	return r
}

// HandleInbound is the M3-pinned entry point (exact signature):
//
//	func (r *Router) HandleInbound(userID, sessionID, deviceClass string, frame []byte)
//
// frame is one proto-marshalled WsMessage. Malformed frames are logged,
// optionally NACKed with a plain SYSTEM_NOTICE, and dropped.
func (r *Router) HandleInbound(userID, sessionID, deviceClass string, frame []byte) {
	msg := &pb.WsMessage{}
	if err := proto.Unmarshal(frame, msg); err != nil {
		r.logger.Warn("router: drop malformed frame",
			zap.String("user", userID), zap.Error(err))
		sendPlainNoticeTo(r.emitter, r.logger, userID, "malformed frame, dropped")
		return
	}
	ctx := &MessageContext{
		UserID:      userID,
		SessionID:   sessionID,
		DeviceClass: deviceClass,
		Msg:         msg,
	}
	terminal := func() error { return r.dispatch(ctx) }
	next := terminal
	for i := len(r.interceptors) - 1; i >= 0; i-- {
		cur, nxt := r.interceptors[i], next
		next = func() error { return cur(ctx, nxt) }
	}
	if err := next(); err != nil {
		r.logger.Warn("router: inbound handling failed",
			zap.String("user", userID),
			zap.String("type", msg.GetType().String()),
			zap.Error(err))
	}
}

// dispatch routes post-interceptor frames by type:
// HEARTBEAT_PING -> PONG (no persist); CHAT -> broadcast; PRIVATE_CHAT ->
// relay; ACK / SIGNALING_* / everything else -> log + drop (stage-2/4).
func (r *Router) dispatch(ctx *MessageContext) error {
	switch ctx.Msg.GetType() {
	case pb.MsgType_HEARTBEAT_PING:
		pong := &pb.WsMessage{
			Type:      pb.MsgType_HEARTBEAT_PONG,
			ToUid:     ctx.UserID,
			Timestamp: time.Now().UnixMilli(),
		}
		b, err := proto.Marshal(pong)
		if err != nil {
			return err
		}
		r.emitter.SendToUser(ctx.UserID, b)
		return nil
	case pb.MsgType_CHAT:
		return r.strategy.RouteBroadcast(ctx)
	case pb.MsgType_PRIVATE_CHAT:
		if ctx.Msg.GetToUid() == "" {
			r.logger.Warn("router: drop private chat without recipient",
				zap.String("user", ctx.UserID))
			sendPlainNoticeTo(r.emitter, r.logger, ctx.UserID, "missing recipient, dropped")
			return nil
		}
		return r.strategy.RoutePrivate(ctx)
	default:
		r.logger.Debug("router: drop unsupported type",
			zap.String("user", ctx.UserID),
			zap.String("type", ctx.Msg.GetType().String()))
		return nil
	}
}
