package router

import (
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"golang-im-neo-system/internal/logic/session"
	"golang-im-neo-system/internal/store"
)

// routeSignaling is the P2P signaling fast-path (SIGNALING_OFFER /
// SIGNALING_ANSWER / SIGNALING_CANDIDATE): pure in-memory bypass delivery.
//
// Hard invariants (M2):
//   - ZERO DB POLLUTION: never persister.EnqueueSync, never alloc.Allocate
//     (allocation can trigger a conversations watermark write), never dedup.
//   - Seq stays 0; Timestamp is stamped in memory only.
//   - Content / Payload / Extra / StanzaId pass through verbatim; Type is
//     preserved and FromUid is the security-identity-overwritten sender.
//   - No sender echo (the WebRTC answer completes the handshake; ACK frames
//     belong to the persisted chat pipeline).
//
// Delivery requires a bidirectional accepted friendship first; an offline
// peer drops with a plain sender notice. Rate limiting already applied via
// the chain's first interceptor.
func (r *Router) routeSignaling(ctx *MessageContext) error {
	from := ctx.UserID
	to := ctx.Msg.GetToUid()

	if !r.signalingPairAllowed(from, to) {
		return nil
	}

	if !r.emitter.IsOnline(to) {
		r.logger.Info("router: signaling dropped, peer offline",
			zap.String("from", from), zap.String("to", to))
		sendPlainNoticeTo(r.emitter, r.logger, from, "peer offline, signaling dropped")
		return nil
	}

	// In-memory stamp only: no seq allocation, no persist, no dedup window.
	ctx.Msg.Seq = 0
	ctx.Msg.Timestamp = time.Now().UnixMilli()
	b, err := proto.Marshal(ctx.Msg)
	if err != nil {
		r.logger.Error("router: marshal signaling failed",
			zap.String("from", from), zap.String("to", to), zap.Error(err))
		return err
	}
	r.emitter.SendToUser(to, b)
	return nil
}

// signalingPairAllowed reports whether from<->to hold accepted friendships in
// BOTH directions (non-deleted rows each way). Deny emits a coded 30001
// notice to the sender; DB errors fail closed with 50001. The pair verdict
// is cached in the shared friendCache ONLY when both directions pass (deny
// verdicts are never written by this path, so a later accept takes effect
// immediately); a cached deny inherited from the private-chat check still
// denies, since a failed single direction implies a failed pair.
func (r *Router) signalingPairAllowed(from, to string) bool {
	key := session.BuildPrivateCovID(from, to)
	if allowed, ok := r.friends.get(key); ok {
		if !allowed {
			sendCodeNoticeTo(r.emitter, r.logger, from,
				CodeFriendNotFound, "friend not found", "not friends, signaling blocked")
			return false
		}
		return true
	}
	var cnt int64
	q := r.db.Model(&store.Friendship{}).
		Where("(user_id = ? AND friend_id = ? OR user_id = ? AND friend_id = ?) AND status = 'accepted'",
			from, to, to, from).
		Where("deleted_at IS NULL")
	if err := q.Count(&cnt).Error; err != nil {
		r.logger.Error("router: signaling friendship lookup failed",
			zap.String("from", from), zap.String("to", to), zap.Error(err))
		sendCodeNoticeTo(r.emitter, r.logger, from,
			CodeInternalError, "internal error", "internal error, please retry")
		return false
	}
	if cnt == 2 {
		r.friends.add(key, true)
		return true
	}
	r.logger.Info("router: signaling blocked, not mutual friends",
		zap.String("from", from), zap.String("to", to))
	sendCodeNoticeTo(r.emitter, r.logger, from,
		CodeFriendNotFound, "friend not found", "not friends, signaling blocked")
	return false
}
