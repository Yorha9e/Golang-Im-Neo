package util

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NewUserID generates a stable user ID like "u20250329abc123" using UUIDv4.
func NewUserID() string {
	return "u_" + uuid.NewString()[:8]
}

// NewSessionID generates a session ID like "s_xxxxxxxx".
func NewSessionID() string {
	return "s_" + uuid.NewString()
}

// NewStanzaID generates a client stanza_id (UUID) for dedup.
func NewStanzaID() string {
	return uuid.NewString()
}

// NewMessageID generates a media mid like "m_20250329_abc123".
func NewMediaID() string {
	return fmt.Sprintf("m_%s_%s", time.Now().Format("20060102"), uuid.NewString()[:8])
}
