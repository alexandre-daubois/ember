package model

type LeakWatcher struct {
	windowSize int
	threshold  int64 // bytes
	samples    map[int]*ringBuffer
}

type ringBuffer struct {
	data []int64
	pos  int
	full bool
}

type LeakStatus struct {
	Leaking bool
	Slope   float64 // bytes per sample
	Samples []int64
	MinMem  int64
	MaxMem  int64
}

func NewLeakWatcher(windowSize int, thresholdMB int) *LeakWatcher {
	return &LeakWatcher{
		windowSize: windowSize,
		threshold:  int64(thresholdMB) * 1024 * 1024,
		samples:    make(map[int]*ringBuffer),
	}
}

func (lw *LeakWatcher) Record(threadIndex int, memoryUsage int64) {
	if memoryUsage <= 0 {
		return
	}
	rb, ok := lw.samples[threadIndex]
	if !ok {
		rb = newRingBuffer(lw.windowSize)
		lw.samples[threadIndex] = rb
	}
	rb.push(memoryUsage)
}

func (lw *LeakWatcher) Status(threadIndex int) LeakStatus {
	rb, ok := lw.samples[threadIndex]
	if !ok {
		return LeakStatus{}
	}

	samples := rb.values()
	if len(samples) < 3 {
		return LeakStatus{Samples: samples}
	}

	minMem, maxMem := samples[0], samples[0]
	for _, v := range samples[1:] {
		if v < minMem {
			minMem = v
		}
		if v > maxMem {
			maxMem = v
		}
	}

	slope := linearSlope(samples)
	drift := maxMem - minMem
	leaking := slope > 0 && drift >= lw.threshold

	return LeakStatus{
		Leaking: leaking,
		Slope:   slope,
		Samples: samples,
		MinMem:  minMem,
		MaxMem:  maxMem,
	}
}

// linearSlope computes the slope of a simple linear regression on the samples.
func linearSlope(values []int64) float64 {
	n := float64(len(values))
	if n < 2 {
		return 0
	}

	var sumX, sumY, sumXY, sumX2 float64
	for i, v := range values {
		x := float64(i)
		y := float64(v)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denom
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{data: make([]int64, size)}
}

func (rb *ringBuffer) push(v int64) {
	rb.data[rb.pos] = v
	rb.pos = (rb.pos + 1) % len(rb.data)
	if rb.pos == 0 {
		rb.full = true
	}
}

func (rb *ringBuffer) values() []int64 {
	if !rb.full {
		return append([]int64{}, rb.data[:rb.pos]...)
	}
	result := make([]int64, len(rb.data))
	copy(result, rb.data[rb.pos:])
	copy(result[len(rb.data)-rb.pos:], rb.data[:rb.pos])
	return result
}
