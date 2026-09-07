package gateway

import (
	"sync"

	"go.uber.org/zap"
)

// Hub maintains the set of active clients and broadcasts messages.
// It uses 32 sharded buckets for zero-lock-contention registration (TECH_STACK_SPECIFICATION mechanism 7).
// For Gate-0 we provide a simplified hub with single map + RWMutex but preserve the sharded interface for future extension.
// The critical Gate-0 primitive preserved here is the backpressure breaker in Client.Enqueue (select default).

const ShardCount = 32

type Hub struct {
	// Sharded buckets: each bucket has its own lock to avoid global contention.
	buckets [ShardCount]bucket
	Broadcast chan []byte
	register   chan *Client
	unregister chan *Client
	logger     *zap.Logger
}

type bucket struct {
	sync.RWMutex
	clients map[string]*Client // key: sessionID or userID+deviceClass
}

func NewHub(logger *zap.Logger) *Hub {
	h := &Hub{
		Broadcast:  make(chan []byte, 1024),
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
			// DeviceClass matrix: hardware and interactive coexist, interactive kicks previous interactive.
			// For Gate-0, we store per sessionID to allow multi-device coexistence.
			shard.clients[client.SessionID] = client
			shard.Unlock()
			h.logger.Info("gateway client registered", zap.String("user", client.UserID), zap.String("session", client.SessionID))

		case client := <-h.unregister:
			shard := h.shardFor(client.UserID)
			shard.Lock()
			if _, ok := shard.clients[client.SessionID]; ok {
				delete(shard.clients, client.SessionID)
				close(client.Send)
			}
			shard.Unlock()
			h.logger.Info("gateway client unregistered", zap.String("user", client.UserID))

		case message := <-h.Broadcast:
			// Marshal-Once is done by the caller (router) — hub just fans out with backpressure breaker.
			h.broadcastMessage(message)
		}
	}
}

func (h *Hub) shardFor(userID string) *bucket {
	// Simple hash: sum bytes % ShardCount
	var sum int
	for i := 0; i < len(userID); i++ {
		sum += int(userID[i])
	}
	return &h.buckets[sum%ShardCount]
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
// It uses the backpressure breaker: slow clients are kicked rather than blocking the broadcast.
func (h *Hub) SendToUser(userID string, msg []byte) bool {
	sent := false
	for i := 0; i < ShardCount; i++ {
		h.buckets[i].RLock()
		for _, c := range h.buckets[i].clients {
			if c.UserID == userID {
				if ok := c.Enqueue(msg); !ok {
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

// BroadcastMessage sends to all clients with backpressure protection.
func (h *Hub) broadcastMessage(msg []byte) {
	for i := 0; i < ShardCount; i++ {
		h.buckets[i].RLock()
		for _, c := range h.buckets[i].clients {
			if ok := c.Enqueue(msg); !ok {
				go h.kickClient(c)
			}
		}
		h.buckets[i].RUnlock()
	}
}

// kickClient forcibly closes and unregisters a slow client.
func (h *Hub) kickClient(c *Client) {
	h.logger.Warn("kicking slow client (backpressure)", zap.String("user", c.UserID), zap.String("session", c.SessionID))
	c.Conn.Close()
	h.Unregister(c)
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
