package gateway

import (
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// Client represents a single WebSocket connection managed by the gateway layer.
// It implements the Gateway's readPump/writePump separation, heartbeat, and backpressure handling.
type Client struct {
	UserID      string
	DeviceClass string // interactive | hardware
	SessionID   string
	Conn        *websocket.Conn
	Send        chan []byte // buffered send channel, size 256 (backpressure threshold)

	hub    *Hub
	logger *zap.Logger
}

// Constants for gateway safety primitives (Gate-0 requirements).
const (
	// 64KB global read limit (Gate 0-4).
	ReadLimit = 64 * 1024

	// Write buffer threshold for backpressure breaker: 256 frames.
	SendBufferSize = 256

	// Timeouts for heartbeats.
	WriteWait      = 10 * time.Second
	PongWait       = 60 * time.Second
	PingPeriod     = 30 * time.Second // server pushes Ping every 30s
	MaxMessageSize = ReadLimit
)

// NewClient creates a new gateway client.
func NewClient(hub *Hub, conn *websocket.Conn, userID, deviceClass, sessionID string, logger *zap.Logger) *Client {
	return &Client{
		UserID:      userID,
		DeviceClass: deviceClass,
		SessionID:   sessionID,
		Conn:        conn,
		Send:        make(chan []byte, SendBufferSize),
		hub:         hub,
		logger:      logger,
	}
}

// ReadPump pumps messages from the WebSocket connection to the hub.
// It enforces SetReadLimit(64KB) and handles Pong.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(ReadLimit)
	c.Conn.SetReadDeadline(time.Now().Add(PongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(PongWait))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Warn("gateway read error", zap.String("user", c.UserID), zap.Error(err))
			}
			break
		}

		// Gateway does NOT interpret business logic — just enforces size (64KB already via SetReadLimit)
		// and forwards raw bytes to the router/hub. Business validation (friendship, stanza_id, etc.) is in router layer.

		// Deliver to hub's broadcast/router channel (non-blocking to avoid blocking readPump on slow router).
		select {
		case c.hub.Broadcast <- message:
		default:
			c.logger.Warn("gateway broadcast channel full, dropping message", zap.String("user", c.UserID))
		}
	}
}

// WritePump pumps messages from the hub to the WebSocket connection.
// It implements the mandatory slow-client backpressure breaker: if Send channel is full, the client is kicked via select default.
func (c *Client) WritePump() {
	ticker := time.NewTicker(PingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(WriteWait))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			w, err := c.Conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Gate-0 fix: do NOT concatenate queued messages with '\n' separator —
			// it corrupts binary Protobuf payloads. Each message is sent as its own
			// WebSocket frame (next WritePump iteration handles the next message).
			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(WriteWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Enqueue attempts to enqueue a message to the client's Send channel with backpressure breaker.
// It uses select default to avoid blocking the broadcaster on slow clients (Gate 0-4).
// Returns true if enqueued, false if the client's buffer is full (caller should kick the client).
// Gate-0 fix: recover from send-on-closed-channel panic caused by concurrent
// Unregister (close(client.Send)) racing with Enqueue (c.Send <- msg).
func (c *Client) Enqueue(msg []byte) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Warn("gateway enqueue on closed channel, dropping message", zap.String("user", c.UserID), zap.String("session", c.SessionID))
			ok = false
		}
	}()
	select {
	case c.Send <- msg:
		return true
	default:
		// Backpressure breaker: buffer full — signal to kick.
		c.logger.Warn("gateway backpressure breaker tripped, buffer full", zap.String("user", c.UserID), zap.String("session", c.SessionID))
		return false
	}
}
