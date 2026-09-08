// One-time WebSocket handshake ticket service (single-process, Stage-1).
//
// The ticket keeps JWTs out of URL query strings: the client trades its
// access token for a 30s single-use ticket via POST /auth/ticket, then the
// handshake layer redeems the ticket verbatim through Redeem.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TicketTTL is the default ticket lifetime (30s per contract).
const TicketTTL = 30 * time.Second

// ticketSweepInterval is how often the background cleanup loop scans for
// expired tickets. One minute keeps the sweep cheap relative to the 30s TTL
// while bounding how long dead entries linger in memory.
const ticketSweepInterval = time.Minute

type ticketEntry struct {
	userID      string
	username    string
	role        string
	sessionID   string
	deviceClass string
	expiresAt   time.Time
}

// TicketService issues one-time WS handshake tickets from an in-memory store.
// It is safe for concurrent use. Expiry is enforced lazily on Redeem, and a
// background cleanup loop sweeps expired entries every minute so dead tickets
// do not linger in memory.
type TicketService struct {
	items sync.Map // ticket string -> ticketEntry

	// TTL overrides the default 30s lifetime. Zero means TicketTTL.
	// Exported so tests can shrink the window without changing production code.
	TTL time.Duration

	stopChan chan struct{}
	stopOnce sync.Once
}

// NewTicketService builds a TicketService with the default 30s TTL and starts
// the background expiry sweeper.
func NewTicketService() *TicketService {
	s := &TicketService{TTL: TicketTTL, stopChan: make(chan struct{})}
	go s.cleanupLoop()
	return s
}

func (s *TicketService) ttl() time.Duration {
	if s == nil || s.TTL <= 0 {
		return TicketTTL
	}
	return s.TTL
}

// Issue mints a single-use ticket ("tkt_" + 32 lowercase-hex chars from
// crypto/rand) bound to the caller's identity, valid for the service TTL.
func (s *TicketService) Issue(userID, username, role, sessionID, deviceClass string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is ~impossible; fall back to UUID hex so Issue
		// never returns an empty ticket.
		u := strings.ReplaceAll(uuid.NewString(), "-", "")
		ticket := "tkt_" + u[:32]
		s.items.Store(ticket, ticketEntry{
			userID: userID, username: username, role: role,
			sessionID: sessionID, deviceClass: deviceClass,
			expiresAt: time.Now().Add(s.ttl()),
		})
		return ticket
	}
	ticket := "tkt_" + hex.EncodeToString(b[:])
	s.items.Store(ticket, ticketEntry{
		userID:      userID,
		username:    username,
		role:        role,
		sessionID:   sessionID,
		deviceClass: deviceClass,
		expiresAt:   time.Now().Add(s.ttl()),
	})
	return ticket
}

// Redeem consumes a ticket single-use (load-AND-delete).
// Unknown or expired tickets return a 10002 typed error.
func (s *TicketService) Redeem(ticket string) (userID, username, role, sessionID, deviceClass string, err error) {
	v, ok := s.items.LoadAndDelete(ticket)
	if !ok {
		return "", "", "", "", "", NewAuthError(CodeUnauthorized, "invalid or expired ticket")
	}
	e, ok := v.(ticketEntry)
	if !ok {
		return "", "", "", "", "", NewAuthError(CodeUnauthorized, "invalid or expired ticket")
	}
	if time.Now().After(e.expiresAt) {
		return "", "", "", "", "", NewAuthError(CodeUnauthorized, "ticket expired")
	}
	return e.userID, e.username, e.role, e.sessionID, e.deviceClass, nil
}

// cleanupLoop runs in the background (started by NewTicketService) and sweeps
// expired tickets every minute until Stop closes stopChan.
func (s *TicketService) cleanupLoop() {
	ticker := time.NewTicker(ticketSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.sweep()
		case <-s.stopChan:
			return
		}
	}
}

// sweep deletes every entry whose expiry is in the past.
func (s *TicketService) sweep() {
	s.items.Range(func(k, v any) bool {
		e, ok := v.(ticketEntry)
		if !ok {
			s.items.Delete(k)
			return true
		}
		if time.Now().After(e.expiresAt) {
			s.items.Delete(k)
		}
		return true
	})
}

// Stop terminates the background expiry sweeper. It is idempotent and safe to
// call multiple times or on a zero-value TicketService.
func (s *TicketService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.stopChan != nil {
			close(s.stopChan)
		}
	})
}
