package router

import (
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	pb "golang-im-neo-system/proto"

	"golang-im-neo-system/internal/logic/group"
	"golang-im-neo-system/internal/logic/session"
	"golang-im-neo-system/internal/store"
)

// persistTimeout bounds the synchronous persist wait (pinned 2s). On timeout
// the sender gets a 50001 SYSTEM_NOTICE; the message may still land later via
// the async writer, in which case client retry dedups on stanza_id (漏洞6).
const persistTimeout = 2 * time.Second

// RelayStrategy is the delivery-strategy slot (SSOT 层级3 verbatim).
// Stage-1 ships CentralRelayStrategy; P2PStrategy is a stage-4 slot.
// M2 adds RouteGroup (persisted group fan-out with echo-to-sender).
type RelayStrategy interface {
	RoutePrivate(ctx *MessageContext) error
	RouteBroadcast(ctx *MessageContext) error
	RouteGroup(ctx *MessageContext) error
}

// CentralRelayStrategy implements all routes over one shared pipeline:
// covID -> dedup lookup (replay ACK, no persist/push) -> alloc seq ->
// build store.Message -> EnqueueSync (2s) -> Duplicated? ACK-only : ACK + push.
// Only the final push differs per route (1-1 / broadcast / group fan-out).
type CentralRelayStrategy struct {
	emitter   Emitter
	persister Persister
	alloc     *session.Allocator
	db        *gorm.DB
	dedup     *DedupWindows
	logger    *zap.Logger
}

// NewCentralRelayStrategy builds the default central-relay pipeline.
// db backs group fan-out member listing (M2); it may be nil only when
// RouteGroup is never called.
func NewCentralRelayStrategy(emitter Emitter, persister Persister, alloc *session.Allocator, db *gorm.DB, dedup *DedupWindows, logger *zap.Logger) *CentralRelayStrategy {
	if logger == nil {
		logger = zap.NewNop()
	}
	if dedup == nil {
		dedup = NewDedupWindows()
	}
	return &CentralRelayStrategy{
		emitter:   emitter,
		persister: persister,
		alloc:     alloc,
		db:        db,
		dedup:     dedup,
		logger:    logger,
	}
}

// Compile-time slot assertion.
var _ RelayStrategy = (*CentralRelayStrategy)(nil)

// RoutePrivate relays a 1-1 message; covID uses colon dict order
// (session.BuildPrivateCovID, 机制3), chatType "chat".
func (s *CentralRelayStrategy) RoutePrivate(ctx *MessageContext) error {
	to := ctx.Msg.GetToUid()
	covID := session.BuildPrivateCovID(ctx.UserID, to)
	return s.relay(ctx, covID, "chat", to, func(b []byte) error {
		s.emitter.SendToUser(to, b)
		return nil
	})
}

// RouteBroadcast relays a hall message; covID is the const public hall,
// chatType "groupchat".
func (s *CentralRelayStrategy) RouteBroadcast(ctx *MessageContext) error {
	return s.relay(ctx, PublicHallCovID, "groupchat", "", func(b []byte) error {
		s.emitter.Broadcast(b)
		return nil
	})
}

// RouteGroup relays a group message: covID is group.BuildCovID(gid),
// chatType "groupchat", store.Message.ToUID = gid. The shared relay pipeline
// runs verbatim (dedup -> alloc -> EnqueueSync -> ACK -> push); the push
// fans out to every active member via Emitter.SendToUsers. The sender is a
// member, so its own sessions receive the frame (echo-to-sender for
// multi-device timeline alignment; clients dedupe by stanza_id) on top of
// the ACK frame every route emits.
func (s *CentralRelayStrategy) RouteGroup(ctx *MessageContext) error {
	gid := ctx.Msg.GetToUid()
	covID := group.BuildCovID(gid)
	return s.relay(ctx, covID, "groupchat", gid, func(b []byte) error {
		members, err := group.ListActiveMemberIDs(s.db, gid)
		if err != nil {
			s.logger.Error("router: group fan-out member listing failed",
				zap.String("group", gid), zap.Error(err))
			sendCodeNoticeTo(s.emitter, s.logger, ctx.UserID,
				CodeInternalError, "internal error", "internal error, please retry")
			return fmt.Errorf("router: list group members %s: %w", gid, err)
		}
		s.emitter.SendToUsers(members, b)
		return nil
	})
}

func (s *CentralRelayStrategy) relay(ctx *MessageContext, covID, chatType, toUID string, deliver func([]byte) error) error {
	stanza := ctx.Msg.GetStanzaId()
	msgType := ctx.Msg.GetType()

	// 漏洞6 铁律: dedup hit => replay ACK, return. NO persist, NO push.
	if stanza != "" {
		if seq, ts, ok := s.dedup.Lookup(covID, stanza); ok {
			s.sendAck(ctx, stanza, seq, ts)
			return nil
		}
	}

	// 漏洞1: immediate server-side seq + physical timestamp.
	seq, err := s.alloc.Allocate(covID)
	if err != nil {
		s.logger.Error("router: allocate seq failed",
			zap.String("cov", covID), zap.Error(err))
		sendCodeNoticeTo(s.emitter, s.logger, ctx.UserID,
			CodeInternalError, "internal error", "internal error, please retry")
		return fmt.Errorf("router: allocate seq for %s: %w", covID, err)
	}
	now := time.Now()
	tsMillis := now.UnixMilli()
	m := &store.Message{
		CovID:       covID,
		Seq:         seq,
		StanzaID:    stanza,
		ChatType:    chatType,
		FromUID:     ctx.UserID,
		ToUID:       toUID,
		ContentType: 1, // text
		Content:     ctx.Msg.GetContent(),
		Extra:       ctx.Msg.GetExtra(),
		Status:      1,
		Timestamp:   tsMillis,
		CreatedAt:   now,
	}
	// NOTE: allocator lazy-creates the conversations row (chatType default);
	// the message row above always carries the authoritative chatType.

	var res WriteResult
	select {
	case res = <-s.persister.EnqueueSync(m):
	case <-time.After(persistTimeout):
		s.logger.Error("router: persist timeout",
			zap.String("cov", covID), zap.String("stanza", stanza))
		sendCodeNoticeTo(s.emitter, s.logger, ctx.UserID,
			CodeInternalError, "internal error", "internal error, please retry")
		return fmt.Errorf("router: persist timeout cov=%s stanza=%s", covID, stanza)
	}
	if res.Err != nil {
		s.logger.Error("router: persist failed",
			zap.String("cov", covID), zap.String("stanza", stanza), zap.Error(res.Err))
		sendCodeNoticeTo(s.emitter, s.logger, ctx.UserID,
			CodeInternalError, "internal error", "internal error, please retry")
		return fmt.Errorf("router: persist message cov=%s stanza=%s: %w", covID, stanza, res.Err)
	}

	// Remember BEFORE acking so a racing retry finds the window hot.
	// (Empty stanzas are not keyable: still ACK+push, just not deduped.)
	if stanza != "" {
		s.dedup.Store(covID, stanza, res.Seq, res.Timestamp)
	}
	// ACK闭环 (机制2/漏洞1): strict server-echoed seq+ts back to the sender.
	s.sendAck(ctx, stanza, res.Seq, res.Timestamp)

	if res.Duplicated {
		// Post-restart replay path: the row already exists, receivers already
		// got it pre-crash. ACK only — NO second push.
		return nil
	}

	out := &pb.WsMessage{
		Type:      msgType,
		Seq:       res.Seq,
		FromUid:   ctx.UserID,
		ToUid:     toUID,
		Content:   ctx.Msg.GetContent(),
		Timestamp: res.Timestamp,
		StanzaId:  stanza,
		Extra:     ctx.Msg.GetExtra(),
	}
	b, err := proto.Marshal(out)
	if err != nil {
		s.logger.Error("router: marshal outbound failed",
			zap.String("cov", covID), zap.Error(err))
		return err
	}
	return deliver(b)
}

// sendAck emits the ACK闭环 frame: {ACK, seq, ts, stanza, to=sender}.
func (s *CentralRelayStrategy) sendAck(ctx *MessageContext, stanza string, seq, ts int64) {
	ack := &pb.WsMessage{
		Type:      pb.MsgType_ACK,
		Seq:       seq,
		Timestamp: ts,
		StanzaId:  stanza,
		ToUid:     ctx.UserID,
	}
	b, err := proto.Marshal(ack)
	if err != nil {
		s.logger.Error("router: marshal ack failed", zap.Error(err))
		return
	}
	s.emitter.SendToUser(ctx.UserID, b)
}

// ---- sender-notice helpers (shared by strategy / interceptors / router) ----

// noticeExtra is the JSON shape of SYSTEM_NOTICE Extra for coded notices.
type noticeExtra struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// sendCodeNoticeTo delivers a SYSTEM_NOTICE carrying an SSOT code in Extra
// (JSON {"code":N,"msg":"..."}) plus a human hint in Content.
func sendCodeNoticeTo(em Emitter, lg *zap.Logger, toUID string, code int, msg, hint string) {
	extra, _ := json.Marshal(noticeExtra{Code: code, Msg: msg})
	n := &pb.WsMessage{
		Type:      pb.MsgType_SYSTEM_NOTICE,
		ToUid:     toUID,
		Content:   hint,
		Timestamp: time.Now().UnixMilli(),
		Extra:     string(extra),
	}
	b, err := proto.Marshal(n)
	if err != nil {
		lg.Error("router: marshal notice failed", zap.Error(err))
		return
	}
	em.SendToUser(toUID, b)
}

// sendPlainNoticeTo delivers a code-less SYSTEM_NOTICE (human hint only).
// Used for non-SSOT conditions (malformed frames, rate limiting, missing
// recipient) to avoid colliding with the SSOT error-code table.
func sendPlainNoticeTo(em Emitter, lg *zap.Logger, toUID, hint string) {
	n := &pb.WsMessage{
		Type:      pb.MsgType_SYSTEM_NOTICE,
		ToUid:     toUID,
		Content:   hint,
		Timestamp: time.Now().UnixMilli(),
	}
	b, err := proto.Marshal(n)
	if err != nil {
		lg.Error("router: marshal notice failed", zap.Error(err))
		return
	}
	em.SendToUser(toUID, b)
}
