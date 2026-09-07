package gateway

import (
	"hash/fnv"
	"sync"

	"go.uber.org/zap"
)

// Hub maintains the set of active clients and broadcasts messages.
// It uses 32 sharded buckets for zero-lock-contention registration (TECH_STACK_SPECIFICATION mechanism 7).
// The critical Gate-0 primitives preserved here are:
//   - 32 sharded buckets with per-bucket locks
//   - safe-close on unregister (close(Send) with recover, paired with Enqueue's recover)
//   - Enqueue select-default breaker + recover
//   - 64KB SetReadLimit (client.go), 30s ping / 60s pong, binary-safe per-frame writePump
//
// Contract B1 (pinned): *Hub exposes SendToUser, Broadcast, IsOnline, KickUser.
// The old exported Broadcast chan field was renamed away to avoid collision
// with the Broadcast method; fan-out is now synchronous (marshal-once friendly)
// and Run only handles register/unregister.

const ShardCount = 32

// InboundHandler is implemented by the router layer. The gateway delivers
// every inbound binary frame to it synchronously per connection.
// Contract B2 (pinned).
type InboundHandler interface {
	HandleInbound(userID, sessionID, deviceClass string, frame []byte)
}

type Hub struct {
	// Sharded buckets: each bucket has its own lock to avoid global contention.
	buckets [ShardCount]bucket
	register   chan *Client
	unregister chan *Client
	logger     *zap.Logger

	inboundMu sync.RWMutex
	inbound   InboundHandler
}

type bucket struct {
	sync.RWMutex
	clients map[string]*Client // key: sessionID
}

func NewHub(logger *zap.Logger) *Hub {
	if logger == nil {
		logger = zap.NewNop()
	}
	h := &Hub{
		register:   make(chan *Client, 256),
		unregister: make(chan *Client, 256),
		logger:     logger,
	}
	for i := 0; i < ShardCount; i++ {
		h.buckets[i].clients = make(map[string]*Client)
	}
	return h
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			shard := h.shardFor(client.UserID)
			shard.Lock()
			// DeviceClass matrix: hardware and interactive coexist.
			// Stored per sessionID to allow multi-device coexistence.
			shard.clients[client.SessionID] = client
			shard.Unlock()
			h.logger.Info("gateway client registered", zap.String("user", client.UserID), zap.String("session", client.SessionID))

		case client := <-h.unregister:
			shard := h.shardFor(client.UserID)
			shard.Lock()
			if _, ok := shard.clients[client.SessionID]; ok {
				delete(shard.clients, client.SessionID)
				// Gate-0 fix: safe close that cannot panic on double-unregister
				// and pairs with Client.Enqueue's recover to avoid
				// send-on-closed-channel race with concurrent SendToUser/broadcast.
				func() {
					defer func() { _ = recover() }()
					close(client.Send)
				}()
			}
			shard.Unlock()
			h.logger.Info("gateway client unregistered", zap.String("user", client.UserID))
		}
	}
}

func (h *Hub) shardFor(userID string) *bucket {
	// FNV-1a for good spread across 32 shards even for sequential userIDs.
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(userID))
	return &h.buckets[hasher.Sum32()%ShardCount]
}

// SetInboundHandler installs the router's inbound delivery target.
// Contract B2 (pinned).
func (h *Hub) SetInboundHandler(handler InboundHandler) {
	h.inboundMu.Lock()
	defer h.inboundMu.Unlock()
	h.inbound = handler
}

func (h *Hub) getInboundHandler() InboundHandler {
	h.inboundMu.RLock()
	defer h.inboundMu.RUnlock()
	return h.inbound
}

// Register enqueues a client for registration.
func (h *Hub) Register(c *Client) {
	h.register <- c
}

// Unregister enqueues a client for removal.
func (h *Hub) Unregister(c *Client) {
	h.unregister <- c
}

// SendToUser attempts to send a message to a specific user (all sessions).
// It uses the backpressure breaker: slow clients are kicked rather than blocking the sender.
// Hardware (ESP32) clients receive the 256-byte truncated variant when needed;
// interactive clients always get the original marshaled bytes (marshal-once).
func (h *Hub) SendToUser(userID string, msg []byte) bool {
	sent := false
	// Lazily computed hardware variant, shared across this user's hardware sessions.
	var hwVariant []byte
	hwReady := false
	getHardware := func() []byte {
		if !hwReady {
			hwVariant = adaptForHardware(msg)
			hwReady = true
		}
		return hwVariant
	}
	for i := 0; i < ShardCount; i++ {
		h.buckets[i].RLock()
		for _, c := range h.buckets[i].clients {
			if c.UserID == userID {
				payload := msg
				if c.DeviceClass == "hardware" && len(msg) > HardwareFrameThreshold {
					payload = getHardware()
				}
				if ok := c.Enqueue(payload); !ok {
					// Backpressure: kick slow client asynchronously to avoid deadlock in RLock.
					go h.kickClient(c)
				} else {
					sent = true
				}
			}
		}
		h.buckets[i].RUnlock()
	}
	return sent
}

// SendToUsers fans out one marshaled frame to every online session of every
// listed user in a SINGLE pass over the 32 shards. Interactive sessions get
// msg verbatim (marshal-once); hardware sessions (DeviceClass=="hardware"
// with len(msg) > HardwareFrameThreshold) share ONE lazily-computed
// adaptForHardware variant. Slow clients trip the backpressure breaker and
// are kicked via go h.kickClient(c). Returns the count of sessions enqueued.
// (M2 group fan-out; the sender's membership yields echo-to-sender.)
func (h *Hub) SendToUsers(userIDs []string, msg []byte) int {
	if len(userIDs) == 0 {
		return 0
	}
	want := make(map[string]struct{}, len(userIDs))
	for _, id := range userIDs {
		if id != "" {
			want[id] = struct{}{}
		}
	}
	if len(want) == 0 {
		return 0
	}
	// Lazily computed hardware variant, shared across ALL hardware recipients.
	var hwVariant []byte
	hwReady := false
	getHardware := func() []byte {
		if !hwReady {
			hwVariant = adaptForHardware(msg)
			hwReady = true
		}
		return hwVariant
	}
	sent := 0
	for i := 0; i < ShardCount; i++ {
		h.buckets[i].RLock()
		for _, c := range h.buckets[i].clients {
			if _, ok := want[c.UserID]; !ok {
				continue
			}
			payload := msg
			if c.DeviceClass == "hardware" && len(msg) > HardwareFrameThreshold {
				payload = getHardware()
			}
			if ok := c.Enqueue(payload); !ok {
				// Backpressure: kick slow client asynchronously to avoid deadlock in RLock.
				go h.kickClient(c)
			} else {
				sent++
			}
		}
		h.buckets[i].RUnlock()
	}
	return sent
}

// Broadcast fans out a single marshaled frame to every connected client.
// Marshal-Once is done by the caller (router): interactive clients receive the
// exact input bytes; hardware clients receive the truncated variant when needed.
// Contract B1 (pinned).
func (h *Hub) Broadcast(msg []byte) {
	h.broadcastMessage(msg)
}

// broadcastMessage sends to all clients with backpressure protection.
func (h *Hub) broadcastMessage(msg []byte) {
	var hwVariant []byte
	hwReady := false
	getHardware := func() []byte {
		if !hwReady {
			hwVariant = adaptForHardware(msg)
			hwReady = true
		}
		return hwVariant
	}
	for i := 0; i < ShardCount; i++ {
		h.buckets[i].RLock()
		for _, c := range h.buckets[i].clients {
			payload := msg
			if c.DeviceClass == "hardware" && len(msg) > HardwareFrameThreshold {
				payload = getHardware()
			}
			if ok := c.Enqueue(payload); !ok {
				go h.kickClient(c)
			}
		}
		h.buckets[i].RUnlock()
	}
}

// kickClient forcibly closes and unregisters a slow client.
func (h *Hub) kickClient(c *Client) {
	h.logger.Warn("kicking slow client (backpressure)", zap.String("user", c.UserID), zap.String("session", c.SessionID))
	if c.Conn != nil {
		_ = c.Conn.Close()
	}
	h.Unregister(c)
}

// KickUser closes and unregisters ALL sessions of the user, logging the reason.
// Contract B1 (pinned). Synchronous: Count drops before return.
func (h *Hub) KickUser(userID, reason string) {
	h.logger.Info("gateway kick user", zap.String("user", userID), zap.String("reason", reason))
	for i := 0; i < ShardCount; i++ {
		var targets []*Client
		h.buckets[i].RLock()
		for _, c := range h.buckets[i].clients {
			if c.UserID == userID {
				targets = append(targets, c)
			}
		}
		h.buckets[i].RUnlock()
		if len(targets) == 0 {
			continue
		}
		h.buckets[i].Lock()
		for _, c := range targets {
			if _, ok := h.buckets[i].clients[c.SessionID]; ok {
				delete(h.buckets[i].clients, c.SessionID)
				func() {
					defer func() { _ = recover() }()
					close(c.Send)
				}()
			}
		}
		h.buckets[i].Unlock()
		for _, c := range targets {
			if c.Conn != nil {
				_ = c.Conn.Close()
			}
		}
	}
}

// KickSession closes and unregisters exactly ONE connection by sessionID.
// Other sessions of the same user (e.g. an ESP32 hardware session) stay online.
// Synchronous: Count drops before return. Returns true if found/kicked.
func (h *Hub) KickSession(sessionID, reason string) bool {
	h.logger.Info("gateway kick session", zap.String("session", sessionID), zap.String("reason", reason))
	// Clients are keyed by sessionID, so probe each shard directly.
	for i := 0; i < ShardCount; i++ {
		h.buckets[i].Lock()
		c, ok := h.buckets[i].clients[sessionID]
		if !ok {
			h.buckets[i].Unlock()
			continue
		}
		delete(h.buckets[i].clients, sessionID)
		// Safe close mirroring KickUser: cannot panic on double-kick and
		// pairs with Client.Enqueue's recover.
		func() {
			defer func() { _ = recover() }()
			close(c.Send)
		}()
		h.buckets[i].Unlock()
		if c.Conn != nil {
			_ = c.Conn.Close()
		}
		return true
	}
	return false
}

// IsOnline checks if a user has any active session.
func (h *Hub) IsOnline(userID string) bool {
	for i := 0; i < ShardCount; i++ {
		h.buckets[i].RLock()
		for _, c := range h.buckets[i].clients {
			if c.UserID == userID {
				h.buckets[i].RUnlock()
				return true
			}
		}
		h.buckets[i].RUnlock()
	}
	return false
}

// Count returns total online connections.
func (h *Hub) Count() int {
	n := 0
	for i := 0; i < ShardCount; i++ {
		h.buckets[i].RLock()
		n += len(h.buckets[i].clients)
		h.buckets[i].RUnlock()
	}
	return n
}

// ClientCount is an alias for Count.
func (h *Hub) ClientCount() int {
	return h.Count()
}
