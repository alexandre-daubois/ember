package model

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func published(at time.Time, id string, topics []string, data string, private bool) fetcher.LogEntry {
	return fetcher.LogEntry{
		Timestamp: at, Level: "debug", Logger: fetcher.MercureLoggerName, Message: "Update published",
		Mercure: &fetcher.MercureLogFields{UpdateID: id, Type: "book", Topics: topics, Data: data, Private: private},
	}
}

func sent(id string, topics []string) fetcher.LogEntry {
	return fetcher.LogEntry{
		Level: "debug", Logger: fetcher.MercureLoggerName, Message: "Update sent",
		Mercure: &fetcher.MercureLogFields{UpdateID: id, Topics: topics},
	}
}

func TestMercureFeed_TrackPublishedAndSent(t *testing.T) {
	now := time.Now()
	f := NewMercureFeed(10)
	f.Track(published(now, "u1", []string{"demo", "https://example.com/books/1"}, `{"title":"Dune"}`, false))
	f.Track(sent("u1", []string{"demo", "https://example.com/books/1"}))
	f.Track(sent("u1", []string{"demo", "https://example.com/books/1"}))
	f.Track(published(now.Add(time.Second), "u2", []string{"demo"}, "x", true))

	updates := f.Snapshot("", 0)
	require.Len(t, updates, 2)
	assert.Equal(t, "u2", updates[0].ID, "newest first")
	assert.Equal(t, 2, updates[1].Recipients)
	assert.True(t, updates[0].Private)
	assert.Equal(t, int64(2), f.WriteCount())
	assert.Equal(t, 2, f.Len())

	topics := f.Topics()
	SortTopics(topics, SortByTopicCount)
	require.Len(t, topics, 2)
	assert.Equal(t, "demo", topics[0].Pattern)
	assert.Equal(t, 1, topics[0].Topics)
	assert.Equal(t, 2, topics[0].Count)
	assert.Equal(t, 2, topics[0].Recipients)
	assert.Equal(t, 1, topics[0].Private)
	assert.Equal(t, int64((16+1)/2), topics[0].AvgBytes())
	assert.Equal(t, now.Add(time.Second), topics[0].LastSeen)
	assert.Equal(t, 1, topics[1].Count)
}

// A publisher may reuse an update ID. The slot evicted first must not take
// the newer update's recipient mapping down with it.
func TestMercureFeed_DuplicateIDKeepsCountingTheNewerUpdate(t *testing.T) {
	now := time.Now()
	f := NewMercureFeed(2)
	f.Track(published(now, "same", []string{"a"}, "old", false))
	f.Track(published(now, "same", []string{"a"}, "new", false))
	f.Track(published(now, "other", []string{"a"}, "x", false))
	f.Track(sent("same", []string{"a"}))

	updates := f.Snapshot("", 0)
	require.Len(t, updates, 2)
	assert.Equal(t, "new", updates[1].Data)
	assert.Equal(t, 1, updates[1].Recipients)
}

func TestMercureFeed_TopicsFoldIntoPatterns(t *testing.T) {
	now := time.Now()
	f := NewMercureFeed(10)
	f.Track(published(now, "u1", []string{"https://example.com/books/1"}, "x", false))
	f.Track(published(now, "u2", []string{"https://example.com/books/2"}, "x", false))
	f.Track(published(now, "u3", []string{"https://example.com/books/2"}, "x", false))
	f.Track(published(now, "u4", []string{"https://example.com/users/550e8400-e29b-41d4-a716-446655440000"}, "x", false))

	topics := f.Topics()
	SortTopics(topics, SortByTopicCount)
	require.Len(t, topics, 2)
	assert.Equal(t, "https://example.com/books/:id", topics[0].Pattern)
	assert.Equal(t, 2, topics[0].Topics, "two distinct concrete topics")
	assert.Equal(t, 3, topics[0].Count)
	assert.Equal(t, "https://example.com/users/:uuid", topics[1].Pattern)
	assert.Equal(t, 1, topics[1].Topics)
}

func TestMercureFeed_IgnoresOtherLines(t *testing.T) {
	f := NewMercureFeed(10)
	f.Track(fetcher.LogEntry{Logger: fetcher.MercureLoggerName, Message: "New subscriber"})
	f.Track(fetcher.LogEntry{Logger: fetcher.MercureLoggerName, Message: "Update published"})
	f.Track(sent("unknown", []string{"demo"}))

	assert.Equal(t, 0, f.Len())
	assert.Nil(t, f.Topics(), "a delivery for an update published before the capture has no row to count on")
}

func TestMercureFeed_RingEvictsAndForgetsID(t *testing.T) {
	f := NewMercureFeed(2)
	now := time.Now()
	f.Track(published(now, "u1", []string{"a"}, "", false))
	f.Track(published(now, "u2", []string{"a"}, "", false))
	f.Track(published(now, "u3", []string{"a"}, "", false))
	f.Track(sent("u1", []string{"a"}))

	assert.Equal(t, int64(1), f.Dropped())
	ids := []string{f.Snapshot("", 0)[0].ID, f.Snapshot("", 0)[1].ID}
	assert.Equal(t, []string{"u3", "u2"}, ids)
	for _, u := range f.Snapshot("", 0) {
		assert.Equal(t, 0, u.Recipients, "a delivery for an evicted update must not land on the slot's new tenant")
	}
	assert.Equal(t, 3, f.Topics()[0].Count, "topic stats outlive the ring")
}

func TestMercureFeed_SnapshotFilterAndLimit(t *testing.T) {
	f := NewMercureFeed(10)
	now := time.Now()
	f.Track(published(now, "u1", []string{"orders"}, `{"status":"paid"}`, false))
	f.Track(published(now, "u2", []string{"users"}, "hello", false))
	f.Track(published(now, "u3", []string{"orders"}, "bye", false))

	assert.Len(t, f.Snapshot("ORDERS", 0), 2)
	assert.Len(t, f.Snapshot("paid", 0), 1)
	assert.Len(t, f.Snapshot("book", 0), 3, "type matches too")
	assert.Len(t, f.Snapshot("", 2), 2)
	assert.Empty(t, f.Snapshot("nothing", 0))
}

func TestMercureFeed_DataCap(t *testing.T) {
	f := NewMercureFeed(10)
	big := strings.Repeat("x", mercureDataCap+5)
	f.Track(published(time.Now(), "u1", []string{"a"}, big, false))

	u := f.Snapshot("", 0)[0]
	assert.Len(t, u.Data, mercureDataCap)
	assert.True(t, u.DataTruncated)
	assert.Equal(t, int64(mercureDataCap+5), f.Topics()[0].SumBytes, "the topic size counts the real payload")
}

func TestMercureFeed_Clear(t *testing.T) {
	f := NewMercureFeed(2)
	now := time.Now()
	for i := 0; i < 3; i++ {
		f.Track(published(now, fmt.Sprintf("u%d", i), []string{"a"}, "", false))
	}
	f.Clear()

	assert.Equal(t, 0, f.Len())
	assert.Equal(t, int64(0), f.Dropped())
	assert.Nil(t, f.Topics())
	f.Track(published(now, "u9", []string{"a"}, "", false))
	f.Track(sent("u9", []string{"a"}))
	assert.Equal(t, 1, f.Snapshot("", 0)[0].Recipients)
}

func TestSortTopics(t *testing.T) {
	now := time.Now()
	stats := []TopicStat{
		{Pattern: "b", Count: 2, Recipients: 1, LastSeen: now},
		{Pattern: "a", Count: 2, Recipients: 5, LastSeen: now.Add(-time.Minute)},
		{Pattern: "c", Count: 9, Recipients: 0, LastSeen: now.Add(time.Minute)},
	}
	first := func(by TopicSortField) string {
		SortTopics(stats, by)
		return stats[0].Pattern
	}
	assert.Equal(t, "c", first(SortByTopicCount))
	assert.Equal(t, "a", stats[1].Pattern, "ties break on the pattern")
	assert.Equal(t, "a", first(SortByTopicName))
	assert.Equal(t, "a", first(SortByTopicRecipients))
	assert.Equal(t, "c", first(SortByTopicLastSeen))

	assert.Equal(t, SortByTopicName, SortByTopicCount.Next())
	assert.Equal(t, SortByTopicLastSeen, SortByTopicCount.Prev())
	assert.Equal(t, "sent", SortByTopicRecipients.String())
	assert.Equal(t, SortByTopicCount, TopicSortField(42).Next())
}
