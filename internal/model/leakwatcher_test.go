package model

import (
	"testing"
)

func TestLeakWatcher_NoSamples(t *testing.T) {
	lw := NewLeakWatcher(20, 5)
	s := lw.Status(0)
	if s.Leaking {
		t.Error("should not be leaking with no samples")
	}
	if len(s.Samples) != 0 {
		t.Errorf("expected 0 samples, got %d", len(s.Samples))
	}
}

func TestLeakWatcher_TooFewSamples(t *testing.T) {
	lw := NewLeakWatcher(20, 5)
	lw.Record(0, 8*1024*1024)
	lw.Record(0, 9*1024*1024)

	s := lw.Status(0)
	if s.Leaking {
		t.Error("should not detect leak with < 3 samples")
	}
	if len(s.Samples) != 2 {
		t.Errorf("expected 2 samples, got %d", len(s.Samples))
	}
}

func TestLeakWatcher_StableMemory(t *testing.T) {
	lw := NewLeakWatcher(10, 5)
	mem := int64(8 * 1024 * 1024)
	for range 10 {
		lw.Record(0, mem)
	}

	s := lw.Status(0)
	if s.Leaking {
		t.Error("stable memory should not be detected as leak")
	}
}

func TestLeakWatcher_DetectsLeak(t *testing.T) {
	lw := NewLeakWatcher(10, 5)
	for i := range 10 {
		mem := int64((8 + i) * 1024 * 1024) // 8MB -> 17MB
		lw.Record(0, mem)
	}

	s := lw.Status(0)
	if !s.Leaking {
		t.Error("should detect leak: 9MB drift with 5MB threshold")
	}
	if s.Slope <= 0 {
		t.Errorf("slope should be positive, got %v", s.Slope)
	}
}

func TestLeakWatcher_BelowThreshold(t *testing.T) {
	lw := NewLeakWatcher(10, 5)
	for i := range 10 {
		mem := int64(8*1024*1024 + i*100*1024) // ~1MB total drift
		lw.Record(0, mem)
	}

	s := lw.Status(0)
	if s.Leaking {
		t.Error("drift below threshold should not be flagged as leak")
	}
}

func TestLeakWatcher_SkipsZeroMemory(t *testing.T) {
	lw := NewLeakWatcher(10, 5)
	lw.Record(0, 0)
	lw.Record(0, 0)

	s := lw.Status(0)
	if len(s.Samples) != 0 {
		t.Errorf("zero memory should be ignored, got %d samples", len(s.Samples))
	}
}

func TestLeakWatcher_MultipleThreads(t *testing.T) {
	lw := NewLeakWatcher(10, 5)

	for i := range 10 {
		lw.Record(0, int64(8*1024*1024))     // stable
		lw.Record(1, int64((8+i)*1024*1024)) // leaking
	}

	s0 := lw.Status(0)
	s1 := lw.Status(1)

	if s0.Leaking {
		t.Error("thread 0 should not be leaking")
	}
	if !s1.Leaking {
		t.Error("thread 1 should be leaking")
	}
}

func TestLeakWatcher_RingBufferWraparound(t *testing.T) {
	lw := NewLeakWatcher(5, 5)

	// fill with stable values first
	for range 5 {
		lw.Record(0, 8*1024*1024)
	}
	// then push increasing values that overwrite
	for i := range 5 {
		lw.Record(0, int64((8+i*2)*1024*1024))
	}

	s := lw.Status(0)
	if len(s.Samples) != 5 {
		t.Errorf("expected 5 samples after wraparound, got %d", len(s.Samples))
	}
	if !s.Leaking {
		t.Error("should detect leak after wraparound")
	}
}

func TestLinearSlope_Increasing(t *testing.T) {
	slope := linearSlope([]int64{1, 2, 3, 4, 5})
	if slope != 1.0 {
		t.Errorf("expected slope 1.0, got %v", slope)
	}
}

func TestLinearSlope_Flat(t *testing.T) {
	slope := linearSlope([]int64{5, 5, 5, 5})
	if slope != 0 {
		t.Errorf("expected slope 0, got %v", slope)
	}
}

func TestLinearSlope_SingleValue(t *testing.T) {
	slope := linearSlope([]int64{42})
	if slope != 0 {
		t.Errorf("expected slope 0 for single value, got %v", slope)
	}
}
