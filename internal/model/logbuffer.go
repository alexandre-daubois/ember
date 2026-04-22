package model

import (
	"sort"
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
	mu         sync.RWMutex
	entries    []fetcher.LogEntry
	capacity   int
	head       int   // next write index
	full       bool  // true once the buffer has wrapped at least once
	writeCount int64 // monotonic total ever appended; survives Clear so diffs against a snapshot stay meaningful
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
	b.writeCount++
}

// WriteCount returns the total number of entries ever appended, regardless
// of evictions. Useful for diffing a frozen snapshot against live growth.
func (b *LogBuffer) WriteCount() int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.writeCount
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

// Dropped returns the number of entries that have been evicted by the ring
// buffer wrapping. Stays zero until the buffer wraps for the first time.
func (b *LogBuffer) Dropped() int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.full {
		return 0
	}
	return b.writeCount - int64(b.capacity)
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
// URI, message, raw line). Host, when non-empty, further restricts results to
// that exact host — folding the per-host selection into the buffer walk so
// narrow views do not pay for a full-buffer copy + filter pass.
type LogFilter struct {
	Search string
	Host   string
}

// Matches reports whether entry passes the filter. Exposed so callers can
// re-apply the same filtering rules to a pre-captured slice of entries (for
// example, a snapshot taken at the moment the UI is paused).
func (f LogFilter) Matches(e fetcher.LogEntry) bool {
	return f.matches(e)
}

func (f LogFilter) matches(e fetcher.LogEntry) bool {
	if f.Host != "" && e.Host != f.Host {
		return false
	}
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

// UniqueHosts returns the set of distinct non-empty Host values currently in
// the buffer, sorted alphabetically. Runs under the read lock and walks the
// ring in place so the UI can refresh the sidepanel list without paying for
// a full Snapshot copy on every render.
func (b *LogBuffer) UniqueHosts() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	total := b.head
	if b.full {
		total = b.capacity
	}
	if total == 0 {
		return nil
	}

	seen := make(map[string]struct{}, 8)
	out := make([]string, 0, 8)
	for i := 0; i < total; i++ {
		idx := (b.head - 1 - i + b.capacity) % b.capacity
		h := b.entries[idx].Host
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	sort.Strings(out)
	return out
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
