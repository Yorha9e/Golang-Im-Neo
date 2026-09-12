package handshake

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"golang-im-neo-system/internal/gateway"
)

// TicketVerifier is implemented by the auth layer and consumed structurally
// by the handshake layer. Redeem validates and single-uses a WS ticket,
// returning the bound HandshakeContext fields.
// Contract C (pinned).
type TicketVerifier interface {
	Redeem(ticket string) (userID, username, role, sessionID, deviceClass string, err error)
}

// Handler performs the WS handshake: ticket redemption (401 on failure,
// never upgrading before redeem), origin check (403 via upgrader), upgrade
// (101), and handoff to the gateway pool with read/write pumps.
type Handler struct {
	hub         *gateway.Hub
	verifier    TicketVerifier
	logger      *zap.Logger
	checkOrigin func(r *http.Request) bool
	upgrader    websocket.Upgrader
}

// NewHandler builds a handshake handler.
// checkOrigin == nil selects the secure default: allow requests with no Origin
// (non-browser clients), allow same-host origins and localhost/127.0.0.1/::1
// origins, deny everything else with 403. Pass AllowAllOrigins explicitly for
// local development only.
func NewHandler(hub *gateway.Hub, verifier TicketVerifier, logger *zap.Logger, checkOrigin func(r *http.Request) bool) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}
	if checkOrigin == nil {
		checkOrigin = defaultCheckOrigin
	}
	return &Handler{
		hub:         hub,
		verifier:    verifier,
		logger:      logger,
		checkOrigin: checkOrigin,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     checkOrigin,
		},
	}
}

// AllowAllOrigins unconditionally allows cross-origin upgrades.
//
// WARNING: development use only. Never use in production: it lets any website
// open an authenticated WebSocket with a stolen ticket. Prefer the secure
// default (nil checkOrigin) or a strict allowlist.
func AllowAllOrigins(_ *http.Request) bool { return true }

func defaultCheckOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	ou, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originHost := strings.ToLower(ou.Hostname())
	if originHost == "" {
		return false
	}
	reqHost := strings.ToLower(requestHostname(r.Host))
	if originHost == reqHost {
		return true
	}
	switch originHost {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	// [LAN-DEBUG-FLAG] Local & LAN multi-device debugging toggle:
	// To re-enable LAN/private IP wildcarding (10.x, 172.16-31.x, 192.168.x) for offline testing,
	// simply uncomment the block below. Disabled by default for production & security penetration testing.
	/*
	if ip := net.ParseIP(originHost); ip != nil {
		if ip.IsPrivate() || ip.IsLoopback() {
			return true
		}
	}
	*/
	return false
}

func requestHostname(hostPort string) string {
	if hostPort == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		return h
	}
	// No port (or bare IPv6 without port): strip brackets.
	return strings.Trim(hostPort, "[]")
}

// ServeWS handles GET /ws?ticket=...: redeem-then-upgrade then gateway handoff.
func (h *Handler) ServeWS(c *gin.Context) {
	ticket := c.Query("ticket")
	if ticket == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 10002, "msg": "missing ticket"})
		return
	}
	userID, username, _, sessionID, deviceClass, err := h.verifier.Redeem(ticket)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 10002, "msg": "invalid or expired ticket"})
		return
	}
	// NEVER upgrade before redeem: upgrade happens only here.
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// gorilla already wrote the 403/400 status for origin/handshake failures.
		h.logger.Debug("handshake upgrade failed", zap.Error(err))
		return
	}
	client := gateway.NewClient(h.hub, conn, userID, deviceClass, sessionID, h.logger)
	client.Username = username
	h.hub.Register(client)
	go client.WritePump()
	go client.ReadPump()
}
