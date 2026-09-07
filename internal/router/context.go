// Package router implements the Stage-1 routing & dispatch layer (MISSION M4).
//
// SSOT: docs/DECOUPLED_ARCHITECTURE_SPEC.md 层级3 (Router, RelayStrategy,
// Interceptor chain §3.2), docs/VULNERABILITIES_AND_FIXES.md 漏洞1 (immediate
// seq+ACK) + 漏洞6 (sliding dedup window), docs/TECH_STACK_SPECIFICATION.md
// 机制2 (ACK闭环) + 机制3 (covId 规范序).
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

// SSOT error codes (docs/TECH_SELECTION_AND_CONTRACTS.md). Only codes pinned
// by the SSOT table are emitted in SYSTEM_NOTICE Extra payloads; all other
// router notices carry a human-readable Content with no code.
const (
	// CodeFriendNotFound (30001, ERR_FRIEND_NOT_FOUND): private chat blocked,
	// sender is not friends with the recipient.
	CodeFriendNotFound = 30001
	// CodeInternalError (50001, ERR_INTERNAL_SERVER): seq allocation or
	// persistence failed.
	CodeInternalError = 50001
)

// PublicHallCovID is the canonical conversation id for public hall broadcast.
const PublicHallCovID = "cov:public:hall"

// WriteResult mirrors the pinned Contract A shape:
//
//	store.WriteResult struct { Seq int64; Timestamp int64; Duplicated bool; Err error }
//
// NOTE (cross-mission M1): M1's store.WriteResult is not on main yet, so this
// package defines the identical shape locally to stay compilable and testable
// in isolation (scope forbids touching internal/store). Field names and types
// match the pin EXACTLY. Once M1 lands, replace this struct with
// `type WriteResult = store.WriteResult` (one line) — zero other changes —
// at which point M1's *store.BatchWriter satisfies Persister structurally.
type WriteResult struct {
	Seq        int64
	Timestamp  int64
	Duplicated bool
	Err        error
}

// Persister is the outbound persistence port (Contract A, shape-pinned).
// The production implementation is M1's *store.BatchWriter.EnqueueSync
// (wired by the integrator once M1 lands, see WriteResult note above).
type Persister interface {
	EnqueueSync(msg *store.Message) <-chan WriteResult
}

// Emitter is the outbound delivery port (Contract B, verbatim).
// The production implementation is M3's *gateway.Hub (structural match).
type Emitter interface {
	SendToUser(userID string, msg []byte) bool
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
