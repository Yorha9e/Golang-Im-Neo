package gateway

import (
	"unicode/utf8"

	improto "golang-im-neo-system/proto"

	"google.golang.org/protobuf/proto"
)

// Hardware (ESP32) truncation adapter (TECH_SELECTION_AND_CONTRACTS §1).
//
// When delivering a frame to a client whose DeviceClass == "hardware" and the
// marshaled frame exceeds HardwareFrameThreshold (256 bytes), the gateway
// unmarshals proto.WsMessage, truncates Content to at most 256 BYTES on a
// UTF-8 rune boundary with suffix TruncateSuffix fitting inside the 256-byte
// budget, re-marshals, and delivers that variant to that client only.
//
// Frames <= 256 bytes pass through untouched (same slice, zero copy).
// Interactive clients always get the original marshaled bytes, preserving
// Marshal-Once for the common case.

const (
	// HardwareFrameThreshold mirrors the ESP32 256-byte content budget.
	HardwareFrameThreshold = 256
	// HardwareContentBudget is the max Content bytes delivered to hardware.
	HardwareContentBudget = 256
	// TruncateSuffix is appended when hardware truncation occurs.
	TruncateSuffix = "...[长消息截断]"
)

// adaptForHardware returns the frame to deliver to a hardware client.
// Small frames return the input slice unchanged; large frames return a
// re-marshaled truncated variant (or the original on unmarshal/marshal failure).
func adaptForHardware(frame []byte) []byte {
	if len(frame) <= HardwareFrameThreshold {
		return frame
	}
	var m improto.WsMessage
	if err := proto.Unmarshal(frame, &m); err != nil {
		return frame
	}
	m.Content = truncateContentForHardware(m.Content)
	out, err := proto.Marshal(&m)
	if err != nil {
		return frame
	}
	return out
}

// truncateContentForHardware caps content to HardwareContentBudget bytes with
// the truncation suffix fitting inside the budget, cut on a rune boundary.
func truncateContentForHardware(content string) string {
	suffixLen := len(TruncateSuffix)
	budget := HardwareContentBudget
	prefixBudget := budget - suffixLen
	if prefixBudget < 0 {
		prefixBudget = 0
	}
	prefix := truncateToBytesOnRuneBoundary(content, prefixBudget)
	return prefix + TruncateSuffix
}

// truncateToBytesOnRuneBoundary returns the longest prefix of s with at most
// maxBytes bytes, never splitting a UTF-8 rune.
func truncateToBytesOnRuneBoundary(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := s[:maxBytes]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}
