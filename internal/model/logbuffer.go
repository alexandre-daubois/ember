package model

import (
	"strconv"
	"strings"
	"sync"

	"github.com/alexandre-daubois/ember/internal/fetcher"
)

// DefaultLogBufferCapacity is the number of recent log entries kept in memory
// before the oldest ones are discarded. At ~512 bytes per entry this is
// roughly 5 MB of RAM in the worst case.
const DefaultLogBufferCapacity = 10_000

// LogBuffer is a fixed-size ring buffer of log entries, safe for concurrent
// use from one writer (the tailer goroutine) and multiple readers (the UI).
type LogBuffer struct {
	mu       sync.RWMutex
	entries  []fetcher.LogEntry
	capacity int
	head     int  // next write index
	full     bool // true once the buffer has wrapped at least once
}

// NewLogBuffer creates a buffer that keeps at most capacity entries.
// A capacity <= 0 falls back to DefaultLogBufferCapacity.
func NewLogBuffer(capacity int) *LogBuffer {
	if capacity <= 0 {
		capacity = DefaultLogBufferCapacity
	}
	return &LogBuffer{
		entries:  make([]fetcher.LogEntry, capacity),
		capacity: capacity,
	}
}

// Append adds one entry, evicting the oldest when the buffer is full.
func (b *LogBuffer) Append(e fetcher.LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.entries[b.head] = e
	b.head = (b.head + 1) % b.capacity
	if b.head == 0 {
		b.full = true
	}
}

// Len returns the number of entries currently stored.
func (b *LogBuffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.full {
		return b.capacity
	}
	return b.head
}

// Full reports whether the buffer has wrapped at least once, meaning older
// entries are being overwritten.
func (b *LogBuffer) Full() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.full
}

// Capacity returns the maximum number of entries the buffer can hold.
func (b *LogBuffer) Capacity() int {
	return b.capacity
}

// Clear empties the buffer without reallocating.
func (b *LogBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.head = 0
	b.full = false
}

// LogFilter restricts the entries returned by Snapshot. Search is matched
// case-insensitively against every visible column (status code, method, host,
// URI, message, raw line).
type LogFilter struct {
	Search string
}

// Matches reports whether entry passes the filter. Exposed so callers can
// re-apply the same filtering rules to a pre-captured slice of entries (for
// example, a snapshot taken at the moment the UI is paused).
func (f LogFilter) Matches(e fetcher.LogEntry) bool {
	return f.matches(e)
}

func (f LogFilter) matches(e fetcher.LogEntry) bool {
	if f.Search != "" {
		needle := strings.ToLower(f.Search)
		if !strings.Contains(strconv.Itoa(e.Status), needle) &&
			!strings.Contains(strings.ToLower(e.Method), needle) &&
			!strings.Contains(strings.ToLower(e.Host), needle) &&
			!strings.Contains(strings.ToLower(e.URI), needle) &&
			!strings.Contains(strings.ToLower(e.Message), needle) &&
			!strings.Contains(strings.ToLower(e.RawLine), needle) {
			return false
		}
	}
	return true
}

// Snapshot returns up to limit entries that match the filter, ordered from
// most recent to oldest. A limit <= 0 returns all matching entries.
func (b *LogBuffer) Snapshot(filter LogFilter, limit int) []fetcher.LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	total := b.head
	if b.full {
		total = b.capacity
	}
	if total == 0 {
		return nil
	}

	cap := total
	if limit > 0 {
		cap = min(total, limit)
	}
	out := make([]fetcher.LogEntry, 0, cap)
	// Walk newest-to-oldest: the most recent write is at head-1.
	for i := 0; i < total; i++ {
		idx := (b.head - 1 - i + b.capacity) % b.capacity
		e := b.entries[idx]
		if !filter.matches(e) {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
