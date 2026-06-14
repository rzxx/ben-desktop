package observability

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

const (
	traceIDHexLen = 32
	spanIDHexLen  = 16
)

func newTraceID() string {
	return randomHex(traceIDHexLen / 2)
}

func newSpanID() string {
	return randomHex(spanIDHexLen / 2)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fallbackRandomHex(n)
	}
	return hex.EncodeToString(buf)
}

func validTraceID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != traceIDHexLen || value == strings.Repeat("0", traceIDHexLen) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validSpanID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != spanIDHexLen || value == strings.Repeat("0", spanIDHexLen) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
