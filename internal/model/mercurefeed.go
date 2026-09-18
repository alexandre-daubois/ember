package model

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
)

// With the data cap below, the worst case is around 32 MB.
const DefaultMercureFeedCapacity = 2_000

// A single update may carry up to the 1 MiB log line limit.
const mercureDataCap = 16 * 1024

// MercureUpdate is one update the hub dispatched. Recipients counts the
// "Update sent" lines seen for the same ID.
type MercureUpdate struct {
	Time          time.Time
	ID            string
	Type          string
	Topics        []string
	Private       bool
	Data          string
	DataTruncated bool
	Recipients    int
}

// TopicStat aggregates the session's updates per topic pattern: the topic run
// through NormalizeURI, so https://example.com/books/12 counts under
// https://example.com/books/:id. Topics is the number of distinct topics behind it.
type TopicStat struct {
	Pattern    string
	Topics     int
	Count      int
	Private    int
	Recipients int
	SumBytes   int64
	LastSeen   time.Time

	seen map[string]struct{}
}

func (s TopicStat) AvgBytes() int64 {
	if s.Count == 0 {
		return 0
	}
	return s.SumBytes / int64(s.Count)
}

// MercureFeed is a ring buffer of hub updates plus per-topic stats, safe for
// one writer and several readers.
type MercureFeed struct {
	mu         sync.RWMutex
	entries    []MercureUpdate
	capacity   int
	head       int
	full       bool
	writeCount int64
	clearBase  int64
	// byID maps an update ID to its slot so "Update sent" lines can find it.
	byID   map[string]int
	topics map[string]*TopicStat
}

func NewMercureFeed(capacity int) *MercureFeed {
	if capacity <= 0 {
		capacity = DefaultMercureFeedCapacity
	}
	return &MercureFeed{
		entries:  make([]MercureUpdate, capacity),
		capacity: capacity,
		byID:     make(map[string]int, capacity),
		topics:   make(map[string]*TopicStat, 32),
	}
}

// Track ignores every line but "Update published" and "Update sent".
func (f *MercureFeed) Track(e fetcher.LogEntry) {
	m := e.Mercure
	if m == nil || m.UpdateID == "" {
		return
	}
	switch e.Message {
	case "Update published":
		f.published(e.Timestamp, m)
	case "Update sent":
		f.sent(m)
	}
}

func (f *MercureFeed) published(at time.Time, m *fetcher.MercureLogFields) {
	u := MercureUpdate{Time: at, ID: m.UpdateID, Type: m.Type, Topics: m.Topics, Private: m.Private, Data: m.Data}
	if len(u.Data) > mercureDataCap {
		u.Data = u.Data[:mercureDataCap]
		u.DataTruncated = true
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// A publisher may reuse an ID; the mapping may already point at a newer slot.
	if old := f.entries[f.head]; old.ID != "" && f.byID[old.ID] == f.head {
		delete(f.byID, old.ID)
	}
	f.entries[f.head] = u
	f.byID[u.ID] = f.head
	f.head = (f.head + 1) % f.capacity
	if f.head == 0 {
		f.full = true
	}
	f.writeCount++

	for _, topic := range m.Topics {
		st := f.topic(topic)
		if _, ok := st.seen[topic]; !ok {
			st.seen[topic] = struct{}{}
			st.Topics++
		}
		st.Count++
		st.SumBytes += int64(len(m.Data))
		if m.Private {
			st.Private++
		}
		if at.After(st.LastSeen) {
			st.LastSeen = at
		}
	}
}

func (f *MercureFeed) sent(m *fetcher.MercureLogFields) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if idx, ok := f.byID[m.UpdateID]; ok {
		f.entries[idx].Recipients++
	}
	// A delivery whose publish line predates the capture has no row to count on.
	for _, topic := range m.Topics {
		if st, ok := f.topics[NormalizeURI(topic)]; ok {
			st.Recipients++
		}
	}
}

func (f *MercureFeed) topic(name string) *TopicStat {
	pattern := NormalizeURI(name)
	st, ok := f.topics[pattern]
	if !ok {
		st = &TopicStat{Pattern: pattern, seen: make(map[string]struct{}, 1)}
		f.topics[pattern] = st
	}
	return st
}

func (f *MercureFeed) Len() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.stored()
}

func (f *MercureFeed) stored() int {
	if f.full {
		return f.capacity
	}
	return f.head
}

func (f *MercureFeed) WriteCount() int64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.writeCount
}

func (f *MercureFeed) Dropped() int64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if !f.full {
		return 0
	}
	return f.writeCount - f.clearBase - int64(f.capacity)
}

func (f *MercureFeed) Clear() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.head = 0
	f.full = false
	f.clearBase = f.writeCount
	clear(f.entries)
	clear(f.byID)
	clear(f.topics)
}

// Matches searches topics, type and data, case-insensitively.
func (u MercureUpdate) Matches(search string) bool {
	if search == "" {
		return true
	}
	needle := strings.ToLower(search)
	if strings.Contains(strings.ToLower(u.Type), needle) || strings.Contains(strings.ToLower(u.Data), needle) {
		return true
	}
	for _, t := range u.Topics {
		if strings.Contains(strings.ToLower(t), needle) {
			return true
		}
	}
	return false
}

// Snapshot returns matching updates newest first; a limit <= 0 returns them all.
func (f *MercureFeed) Snapshot(search string, limit int) []MercureUpdate {
	f.mu.RLock()
	defer f.mu.RUnlock()

	total := f.stored()
	if total == 0 {
		return nil
	}
	out := make([]MercureUpdate, 0, min(total, max(limit, 1)))
	for i := 0; i < total; i++ {
		u := f.entries[(f.head-1-i+f.capacity)%f.capacity]
		if !u.Matches(search) {
			continue
		}
		out = append(out, u)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (f *MercureFeed) Topics() []TopicStat {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if len(f.topics) == 0 {
		return nil
	}
	out := make([]TopicStat, 0, len(f.topics))
	for _, st := range f.topics {
		cp := *st
		cp.seen = nil
		out = append(out, cp)
	}
	return out
}

type TopicSortField int

const (
	SortByTopicCount TopicSortField = iota
	SortByTopicName
	SortByTopicRecipients
	SortByTopicLastSeen
)

var topicSortFieldOrder = []TopicSortField{SortByTopicCount, SortByTopicName, SortByTopicRecipients, SortByTopicLastSeen}

func (s TopicSortField) String() string {
	switch s {
	case SortByTopicName:
		return "pattern"
	case SortByTopicRecipients:
		return "sent"
	case SortByTopicLastSeen:
		return "last seen"
	default:
		return "updates"
	}
}

func (s TopicSortField) Next() TopicSortField {
	for i, f := range topicSortFieldOrder {
		if f == s {
			return topicSortFieldOrder[(i+1)%len(topicSortFieldOrder)]
		}
	}
	return SortByTopicCount
}

func (s TopicSortField) Prev() TopicSortField {
	for i, f := range topicSortFieldOrder {
		if f == s {
			return topicSortFieldOrder[(i-1+len(topicSortFieldOrder))%len(topicSortFieldOrder)]
		}
	}
	return SortByTopicCount
}

// The pattern breaks every tie so rows keep their place between two refreshes.
func SortTopics(stats []TopicStat, by TopicSortField) {
	sort.SliceStable(stats, func(i, j int) bool {
		a, b := stats[i], stats[j]
		switch by {
		case SortByTopicCount:
			if a.Count != b.Count {
				return a.Count > b.Count
			}
		case SortByTopicRecipients:
			if a.Recipients != b.Recipients {
				return a.Recipients > b.Recipients
			}
		case SortByTopicLastSeen:
			if !a.LastSeen.Equal(b.LastSeen) {
				return a.LastSeen.After(b.LastSeen)
			}
		case SortByTopicName:
		}
		return a.Pattern < b.Pattern
	})
}
