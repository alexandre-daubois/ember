package ui

const hostSparklineSize = 8

type historyStore struct {
	rps     []float64
	cpu     []float64
	rss     []float64
	queue   []float64
	busy    []float64
	mem     map[int][]int64
	hostRPS map[string][]float64
}

func newHistoryStore() *historyStore {
	return &historyStore{
		mem:     make(map[int][]int64),
		hostRPS: make(map[string][]float64),
	}
}

func (h *historyStore) appendRPS(val float64)   { h.rps = appendHistory(h.rps, val, graphHistorySize) }
func (h *historyStore) appendCPU(val float64)   { h.cpu = appendHistory(h.cpu, val, graphHistorySize) }
func (h *historyStore) appendRSS(val float64)   { h.rss = appendHistory(h.rss, val, graphHistorySize) }
func (h *historyStore) appendQueue(val float64)  { h.queue = appendHistory(h.queue, val, graphHistorySize) }
func (h *historyStore) appendBusy(val float64)   { h.busy = appendHistory(h.busy, val, graphHistorySize) }

func (h *historyStore) appendHostRPS(host string, rps float64) {
	series := h.hostRPS[host]
	series = append(series, rps)
	if len(series) > hostSparklineSize {
		series = series[len(series)-hostSparklineSize:]
	}
	h.hostRPS[host] = series
}

func (h *historyStore) pruneHosts(activeHosts map[string]struct{}) {
	for host := range h.hostRPS {
		if _, ok := activeHosts[host]; !ok {
			delete(h.hostRPS, host)
		}
	}
}

func (h *historyStore) recordMem(index int, usage int64) {
	if usage <= 0 {
		return
	}
	samples := h.mem[index]
	samples = append(samples, usage)
	if len(samples) > memHistorySize {
		samples = samples[len(samples)-memHistorySize:]
	}
	h.mem[index] = samples
}
