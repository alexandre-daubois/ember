package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLeakWatcher_NoSamples(t *testing.T) {
	lw := NewLeakWatcher(20, 5)
	s := lw.Status(0)
	assert.False(t, s.Leaking, "should not be leaking with no samples")
	assert.Empty(t, s.Samples)
}

func TestLeakWatcher_TooFewSamples(t *testing.T) {
	lw := NewLeakWatcher(20, 5)
	lw.Record(0, 8*1024*1024)
	lw.Record(0, 9*1024*1024)

	s := lw.Status(0)
	assert.False(t, s.Leaking, "should not detect leak with < 3 samples")
	assert.Len(t, s.Samples, 2)
}

func TestLeakWatcher_StableMemory(t *testing.T) {
	lw := NewLeakWatcher(10, 5)
	mem := int64(8 * 1024 * 1024)
	for range 10 {
		lw.Record(0, mem)
	}

	s := lw.Status(0)
	assert.False(t, s.Leaking, "stable memory should not be detected as leak")
}

func TestLeakWatcher_DetectsLeak(t *testing.T) {
	lw := NewLeakWatcher(10, 5)
	for i := range 10 {
		mem := int64((8 + i) * 1024 * 1024) // 8MB -> 17MB
		lw.Record(0, mem)
	}

	s := lw.Status(0)
	assert.True(t, s.Leaking, "should detect leak: 9MB drift with 5MB threshold")
	assert.Positive(t, s.Slope)
}

func TestLeakWatcher_BelowThreshold(t *testing.T) {
	lw := NewLeakWatcher(10, 5)
	for i := range 10 {
		mem := int64(8*1024*1024 + i*100*1024) // ~1MB total drift
		lw.Record(0, mem)
	}

	s := lw.Status(0)
	assert.False(t, s.Leaking, "drift below threshold should not be flagged as leak")
}

func TestLeakWatcher_SkipsZeroMemory(t *testing.T) {
	lw := NewLeakWatcher(10, 5)
	lw.Record(0, 0)
	lw.Record(0, 0)

	s := lw.Status(0)
	assert.Empty(t, s.Samples, "zero memory should be ignored")
}

func TestLeakWatcher_MultipleThreads(t *testing.T) {
	lw := NewLeakWatcher(10, 5)

	for i := range 10 {
		lw.Record(0, int64(8*1024*1024))     // stable
		lw.Record(1, int64((8+i)*1024*1024)) // leaking
	}

	s0 := lw.Status(0)
	s1 := lw.Status(1)

	assert.False(t, s0.Leaking, "thread 0 should not be leaking")
	assert.True(t, s1.Leaking, "thread 1 should be leaking")
}

func TestLeakWatcher_RingBufferWraparound(t *testing.T) {
	lw := NewLeakWatcher(5, 5)

	for range 5 {
		lw.Record(0, 8*1024*1024)
	}
	for i := range 5 {
		lw.Record(0, int64((8+i*2)*1024*1024))
	}

	s := lw.Status(0)
	assert.Len(t, s.Samples, 5)
	assert.True(t, s.Leaking, "should detect leak after wraparound")
}

func TestLinearSlope_Increasing(t *testing.T) {
	slope := linearSlope([]int64{1, 2, 3, 4, 5})
	assert.Equal(t, 1.0, slope)
}

func TestLinearSlope_Flat(t *testing.T) {
	slope := linearSlope([]int64{5, 5, 5, 5})
	assert.Equal(t, float64(0), slope)
}

func TestLinearSlope_SingleValue(t *testing.T) {
	slope := linearSlope([]int64{42})
	assert.Equal(t, float64(0), slope)
}
