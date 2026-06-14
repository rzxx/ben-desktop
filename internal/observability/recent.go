package observability

import (
	"strings"
	"sync"

	apitypes "ben/desktop/api/types"
)

type recentRing struct {
	mu      sync.Mutex
	limit   int
	records []apitypes.TraceRecord
	next    int
	full    bool
}

func newRecentRing(limit int) *recentRing {
	if limit <= 0 {
		limit = 512
	}
	return &recentRing{limit: limit, records: make([]apitypes.TraceRecord, limit)}
}

func (r *recentRing) Add(record apitypes.TraceRecord) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[r.next] = record
	r.next = (r.next + 1) % r.limit
	if r.next == 0 {
		r.full = true
	}
}

func (r *recentRing) Snapshot(filter apitypes.RecentTraceFilter) []apitypes.TraceRecord {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	count := r.next
	if r.full {
		count = r.limit
	}
	out := make([]apitypes.TraceRecord, 0, count)
	for i := 0; i < count; i++ {
		index := i
		if r.full {
			index = (r.next + i) % r.limit
		}
		record := r.records[index]
		if !recordMatches(record, filter) {
			continue
		}
		out = append(out, record)
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[len(out)-filter.Limit:]
	}
	return append([]apitypes.TraceRecord(nil), out...)
}

func (r *recentRing) Len() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full {
		return r.limit
	}
	return r.next
}

func recordMatches(record apitypes.TraceRecord, filter apitypes.RecentTraceFilter) bool {
	if signal := strings.TrimSpace(filter.Signal); signal != "" && !strings.EqualFold(record.Signal, signal) {
		return false
	}
	if service := strings.TrimSpace(filter.Service); service != "" && !strings.EqualFold(record.Service, service) {
		return false
	}
	return true
}
