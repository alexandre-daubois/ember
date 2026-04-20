package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHistoryStore_AppendCapsAtGraphSize(t *testing.T) {
	h := newHistoryStore()
	for i := 0; i < graphHistorySize+50; i++ {
		h.appendRPS(float64(i))
	}
	assert.Len(t, h.rps, graphHistorySize)
	assert.Equal(t, float64(graphHistorySize+49), h.rps[len(h.rps)-1])
}

func TestHistoryStore_AllSeries(t *testing.T) {
	h := newHistoryStore()
	h.appendRPS(1)
	h.appendCPU(2)
	h.appendRSS(3)
	h.appendQueue(4)
	h.appendBusy(5)

	assert.Len(t, h.rps, 1)
	assert.Len(t, h.cpu, 1)
	assert.Len(t, h.rss, 1)
	assert.Len(t, h.queue, 1)
	assert.Len(t, h.busy, 1)
}

func TestHistoryStore_RecordMem(t *testing.T) {
	h := newHistoryStore()
	h.recordMem(0, 5*1024*1024)
	h.recordMem(0, 6*1024*1024)

	assert.Len(t, h.mem[0], 2)
	assert.Equal(t, int64(5*1024*1024), h.mem[0][0])
}

func TestHistoryStore_RecordMemIgnoresZero(t *testing.T) {
	h := newHistoryStore()
	h.recordMem(0, 0)
	h.recordMem(0, -1)

	assert.Empty(t, h.mem[0])
}

func TestHistoryStore_RecordMemCapsAtMax(t *testing.T) {
	h := newHistoryStore()
	for i := 0; i < memHistorySize+20; i++ {
		h.recordMem(0, int64(i+1)*1024)
	}
	assert.Len(t, h.mem[0], memHistorySize)
}

func TestHistoryStore_AppendHostRPS(t *testing.T) {
	h := newHistoryStore()
	for i := 0; i < hostSparklineSize+5; i++ {
		h.appendHostRPS("example.com", float64(i))
	}
	assert.Len(t, h.hostRPS["example.com"], hostSparklineSize)
	assert.Equal(t, float64(5), h.hostRPS["example.com"][0])
	assert.Equal(t, float64(hostSparklineSize+4), h.hostRPS["example.com"][hostSparklineSize-1])
}

func TestHistoryStore_AppendHostRPS_MultipleHosts(t *testing.T) {
	h := newHistoryStore()
	h.appendHostRPS("a.com", 10)
	h.appendHostRPS("b.com", 20)
	h.appendHostRPS("a.com", 30)

	assert.Len(t, h.hostRPS["a.com"], 2)
	assert.Len(t, h.hostRPS["b.com"], 1)
	assert.Equal(t, float64(30), h.hostRPS["a.com"][1])
}

func TestHistoryStore_PruneHosts(t *testing.T) {
	h := newHistoryStore()
	h.appendHostRPS("a.com", 10)
	h.appendHostRPS("b.com", 20)
	h.appendHostRPS("c.com", 30)

	active := map[string]struct{}{"a.com": {}, "c.com": {}}
	h.pruneHosts(active)

	assert.Contains(t, h.hostRPS, "a.com")
	assert.Contains(t, h.hostRPS, "c.com")
	assert.NotContains(t, h.hostRPS, "b.com")
}

func TestHistoryStore_PruneHosts_EmptyActive(t *testing.T) {
	h := newHistoryStore()
	h.appendHostRPS("a.com", 10)

	h.pruneHosts(map[string]struct{}{})

	assert.Empty(t, h.hostRPS)
}

func TestHistoryStore_PruneMem(t *testing.T) {
	h := newHistoryStore()
	h.recordMem(0, 1024)
	h.recordMem(1, 2048)
	h.recordMem(2, 4096)

	active := map[int]struct{}{0: {}, 2: {}}
	h.pruneMem(active)

	assert.Contains(t, h.mem, 0)
	assert.Contains(t, h.mem, 2)
	assert.NotContains(t, h.mem, 1)
}

func TestHistoryStore_PruneMem_EmptyActive(t *testing.T) {
	h := newHistoryStore()
	h.recordMem(0, 1024)
	h.recordMem(1, 2048)

	h.pruneMem(map[int]struct{}{})

	assert.Empty(t, h.mem)
}

func TestLastN_ShorterThanWindowReturnsAll(t *testing.T) {
	got := lastN([]float64{1, 2, 3}, 5)
	assert.Equal(t, []float64{1, 2, 3}, got,
		"a window larger than the slice must return everything, not pad")
}

func TestLastN_LongerThanWindowReturnsTail(t *testing.T) {
	got := lastN([]float64{1, 2, 3, 4, 5}, 3)
	assert.Equal(t, []float64{3, 4, 5}, got)
}

func TestLastN_EqualReturnsSame(t *testing.T) {
	in := []float64{1, 2, 3}
	got := lastN(in, 3)
	assert.Equal(t, in, got)
}
