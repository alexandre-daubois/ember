package model

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeEntry(i int, host, method string, status int) fetcher.LogEntry {
	return fetcher.LogEntry{
		Timestamp: time.Unix(int64(1_700_000_000+i), 0).UTC(),
		Host:      host,
		Method:    method,
		Status:    status,
		URI:       "/path/" + strconv.Itoa(i),
		Message:   "msg " + strconv.Itoa(i),
	}
}

func TestLogBuffer_AppendAndLen(t *testing.T) {
	b := NewLogBuffer(5)

	assert.Equal(t, 0, b.Len())
	assert.Equal(t, 5, b.Capacity())
	assert.False(t, b.Full())

	b.Append(makeEntry(1, "a", "GET", 200))
	b.Append(makeEntry(2, "a", "GET", 200))

	assert.Equal(t, 2, b.Len())
	assert.False(t, b.Full(), "buffer not yet at capacity")
}

func TestLogBuffer_DefaultCapacity(t *testing.T) {
	b := NewLogBuffer(0)
	assert.Equal(t, DefaultLogBufferCapacity, b.Capacity())

	b = NewLogBuffer(-5)
	assert.Equal(t, DefaultLogBufferCapacity, b.Capacity())
}

func TestLogBuffer_Overflow(t *testing.T) {
	b := NewLogBuffer(3)

	for i := 1; i <= 5; i++ {
		b.Append(makeEntry(i, "a", "GET", 200))
	}

	// The oldest two entries (1, 2) must have been evicted.
	assert.Equal(t, 3, b.Len())
	assert.True(t, b.Full(), "buffer should report full after wrapping")

	snap := b.Snapshot(LogFilter{}, 0)
	require.Len(t, snap, 3)
	// Snapshot returns newest-first.
	assert.Equal(t, "/path/5", snap[0].URI)
	assert.Equal(t, "/path/4", snap[1].URI)
	assert.Equal(t, "/path/3", snap[2].URI)
}

func TestLogBuffer_Snapshot_NewestFirst(t *testing.T) {
	b := NewLogBuffer(10)
	for i := 1; i <= 5; i++ {
		b.Append(makeEntry(i, "a", "GET", 200))
	}

	snap := b.Snapshot(LogFilter{}, 0)
	require.Len(t, snap, 5)
	for i, e := range snap {
		expected := "/path/" + strconv.Itoa(5-i)
		assert.Equal(t, expected, e.URI)
	}
}

func TestLogBuffer_Snapshot_Empty(t *testing.T) {
	b := NewLogBuffer(5)
	snap := b.Snapshot(LogFilter{}, 10)
	assert.Nil(t, snap)
}

func TestLogBuffer_Snapshot_Limit(t *testing.T) {
	b := NewLogBuffer(10)
	for i := 1; i <= 5; i++ {
		b.Append(makeEntry(i, "a", "GET", 200))
	}

	snap := b.Snapshot(LogFilter{}, 2)
	require.Len(t, snap, 2)
	assert.Equal(t, "/path/5", snap[0].URI)
	assert.Equal(t, "/path/4", snap[1].URI)
}

func TestLogBuffer_Snapshot_FilterByStatusCode(t *testing.T) {
	b := NewLogBuffer(10)
	b.Append(makeEntry(1, "a", "GET", 200))
	b.Append(makeEntry(2, "a", "GET", 404))
	b.Append(makeEntry(3, "a", "GET", 500))
	b.Append(makeEntry(4, "a", "GET", 502))

	snap := b.Snapshot(LogFilter{Search: "500"}, 0)
	require.Len(t, snap, 1)
	assert.Equal(t, 500, snap[0].Status)

	snap = b.Snapshot(LogFilter{Search: "50"}, 0)
	require.Len(t, snap, 2)
	for _, e := range snap {
		assert.Contains(t, []int{500, 502}, e.Status)
	}
}

func TestLogBuffer_Snapshot_FilterBySearch(t *testing.T) {
	b := NewLogBuffer(10)
	b.Append(fetcher.LogEntry{URI: "/api/users", Host: "a", Message: "ok"})
	b.Append(fetcher.LogEntry{URI: "/api/orders", Host: "a", Message: "ok"})
	b.Append(fetcher.LogEntry{URI: "/other", Host: "API.example.com", Message: "ok"})
	b.Append(fetcher.LogEntry{URI: "/other", Host: "a", Message: "contains API keyword"})
	b.Append(fetcher.LogEntry{URI: "/other", Host: "a", Message: "unrelated", RawLine: "raw api line"})

	snap := b.Snapshot(LogFilter{Search: "api"}, 0)
	assert.Len(t, snap, 5)

	snap = b.Snapshot(LogFilter{Search: "orders"}, 0)
	assert.Len(t, snap, 1)

	snap = b.Snapshot(LogFilter{Search: "missing"}, 0)
	assert.Empty(t, snap)
}

func TestLogBuffer_Snapshot_FilterBySearch_MatchesMethod(t *testing.T) {
	b := NewLogBuffer(10)
	b.Append(fetcher.LogEntry{Method: "GET", Host: "a", URI: "/x"})
	b.Append(fetcher.LogEntry{Method: "POST", Host: "b", URI: "/y"})

	snap := b.Snapshot(LogFilter{Search: "post"}, 0)
	require.Len(t, snap, 1)
	assert.Equal(t, "POST", snap[0].Method)
}

func TestLogBuffer_UniqueHosts_DedupesAndSorts(t *testing.T) {
	b := NewLogBuffer(10)
	b.Append(makeEntry(1, "b.com", "GET", 200))
	b.Append(makeEntry(2, "a.com", "GET", 200))
	b.Append(makeEntry(3, "b.com", "POST", 200))
	b.Append(makeEntry(4, "c.com", "GET", 404))

	assert.Equal(t, []string{"a.com", "b.com", "c.com"}, b.UniqueHosts())
}

func TestLogBuffer_UniqueHosts_SkipsEmpty(t *testing.T) {
	b := NewLogBuffer(10)
	b.Append(fetcher.LogEntry{Host: ""})
	b.Append(fetcher.LogEntry{Host: "real.com"})
	assert.Equal(t, []string{"real.com"}, b.UniqueHosts())
}

func TestLogBuffer_UniqueHosts_Empty(t *testing.T) {
	b := NewLogBuffer(5)
	assert.Nil(t, b.UniqueHosts())
}

func TestLogBuffer_UniqueHosts_AfterWrap(t *testing.T) {
	b := NewLogBuffer(3)
	b.Append(makeEntry(1, "evicted.com", "GET", 200))
	b.Append(makeEntry(2, "kept1.com", "GET", 200))
	b.Append(makeEntry(3, "kept2.com", "GET", 200))
	b.Append(makeEntry(4, "kept3.com", "GET", 200))

	assert.Equal(t, []string{"kept1.com", "kept2.com", "kept3.com"}, b.UniqueHosts())
}

func TestLogBuffer_Clear(t *testing.T) {
	b := NewLogBuffer(5)
	for i := 1; i <= 7; i++ { // triggers wrap
		b.Append(makeEntry(i, "a", "GET", 200))
	}
	require.Equal(t, 5, b.Len())

	b.Clear()

	assert.Equal(t, 0, b.Len())
	assert.Empty(t, b.Snapshot(LogFilter{}, 0))

	// Buffer must stay usable after a clear.
	b.Append(makeEntry(100, "a", "GET", 200))
	assert.Equal(t, 1, b.Len())
	snap := b.Snapshot(LogFilter{}, 0)
	require.Len(t, snap, 1)
	assert.Equal(t, "/path/100", snap[0].URI)
}

func TestLogBuffer_ConcurrentReadersAndWriter(t *testing.T) {
	b := NewLogBuffer(500)

	const writes = 5000
	var wg sync.WaitGroup

	wg.Go(func() {
		for i := range writes {
			b.Append(makeEntry(i, "a", "GET", 200))
		}
	})

	for range 4 {
		wg.Go(func() {
			for range 500 {
				_ = b.Snapshot(LogFilter{Search: "a"}, 50)
				_ = b.Len()
			}
		})
	}

	wg.Wait()

	// After all writes the buffer must be full and consistent.
	assert.Equal(t, 500, b.Len())
	snap := b.Snapshot(LogFilter{}, 0)
	assert.Len(t, snap, 500)
}

func BenchmarkLogBuffer_Append(b *testing.B) {
	buf := NewLogBuffer(10_000)
	e := makeEntry(1, "example.com", "GET", 200)

	b.ReportAllocs()
	for b.Loop() {
		buf.Append(e)
	}
}

func BenchmarkLogBuffer_SnapshotFiltered(b *testing.B) {
	buf := NewLogBuffer(10_000)
	for i := range 10_000 {
		buf.Append(makeEntry(i, fmt.Sprintf("host%d.com", i%50), "GET", 200+(i%4)*100))
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = buf.Snapshot(LogFilter{Search: "host10.com"}, 200)
	}
}
