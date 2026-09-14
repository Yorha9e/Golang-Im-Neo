// Package router implements the Stage-1 routing & dispatch layer (MISSION M4).
//
// SSOT: layer-3 routing design (Router, RelayStrategy, interceptor chain),
// the immediate-seq+ACK and sliding-dedup-window invariants, and the
// ACK-close-loop + canonical covId ordering mechanisms.
//
// Pipeline per inbound frame:
//
//	HandleInbound -> RateLimit -> SecurityIdentity -> FriendshipCheck -> dispatch
//	 -> CentralRelayStrategy (dedup lookup -> alloc seq -> persist -> ACK -> push)
package router

import (
	pb "golang-im-neo-system/proto"

	"golang-im-neo-system/internal/store"
)

// SSOT error codes. Only codes pinned
// by the SSOT table are emitted in SYSTEM_NOTICE Extra payloads; all other
// router notices carry a human-readable Content with no code.
const (
	// CodeFriendNotFound (30001, ERR_FRIEND_NOT_FOUND): private chat blocked,
	// sender is not friends with the recipient.
	CodeFriendNotFound = 30001
	// CodeGroupNotMember (40002, ERR_GROUP_NOT_MEMBER): group chat blocked,
	// sender is not an active member of the target group (M2).
	CodeGroupNotMember = 40002
	// CodeGroupMuted (40005, ERR_GROUP_MUTED): group chat blocked,
	// sender is muted in the target group (M2, live check, never cached).
	CodeGroupMuted = 40005
	// CodeInternalError (50001, ERR_INTERNAL_SERVER): seq allocation or
	// persistence failed.
	CodeInternalError = 50001
)

// PublicHallCovID is the canonical conversation id for public hall broadcast.
const PublicHallCovID = "cov:public:hall"

// WriteResult is the per-message persistence outcome (Contract A).
// It is a type ALIAS for M1's store.WriteResult (merged to main), so the
// production *store.BatchWriter satisfies Persister structurally with zero
// conversion. Keep using this short name inside the router package.
type WriteResult = store.WriteResult

// Persister is the outbound persistence port (Contract A, verbatim).
// Satisfied structurally by M1's *store.BatchWriter (see assertion below).
type Persister interface {
	EnqueueSync(msg *store.Message) <-chan WriteResult
}

// Compile-time Contract A integration proof: the real batch writer is a
// valid Persister (channel element types now identical via the alias).
var _ Persister = (*store.BatchWriter)(nil)

// Emitter is the outbound delivery port (Contract B, verbatim).
// The production implementation is M3's *gateway.Hub (structural match).
// M2 adds SendToUsers for group fan-out (one marshaled frame to every
// session of every listed user; the Hub applies the hardware variant).
type Emitter interface {
	SendToUser(userID string, msg []byte) bool
	SendToUsers(userIDs []string, msg []byte) int
	Broadcast(msg []byte)
	IsOnline(userID string) bool
	KickUser(userID, reason string)
}

// MessageContext carries one inbound frame through the interceptor chain
// into the RelayStrategy. UserID is the gateway-authenticated sender;
// Msg.FromUID is untrusted wire input until SecurityIdentityInterceptor
// overwrites it (defense against spoofing).
type MessageContext struct {
	UserID      string
	SessionID   string
	DeviceClass string
	Msg         *pb.WsMessage
}

// Interceptor is one link of the router chain (SSOT §3.2 verbatim):
// call next() to continue, or return nil early to terminate (drop) after
// handling (e.g. rate-limit / friendship deny with a sender notice).
type Interceptor func(ctx *MessageContext, next func() error) error
