package observability

import (
	"encoding/hex"
	"sync/atomic"
	"time"
)

var fallbackSeq atomic.Uint64

func fallbackRandomHex(n int) string {
	seq := fallbackSeq.Add(1)
	now := uint64(time.Now().UTC().UnixNano())
	buf := make([]byte, n)
	for i := range buf {
		shift := uint((i % 8) * 8)
		buf[i] = byte((now >> shift) ^ (seq >> shift) ^ uint64(i*31))
	}
	return hex.EncodeToString(buf)
}
