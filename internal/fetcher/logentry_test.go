package fetcher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLogLine_FullAccessLog(t *testing.T) {
	line := `{"level":"info","ts":1742472000.123456,"logger":"http.log.access.log0","msg":"handled request","request":{"remote_ip":"192.168.1.1","remote_port":"54321","proto":"HTTP/2.0","method":"GET","host":"example.com","uri":"/api/users"},"bytes_read":0,"duration":0.001234,"size":1234,"status":200,"resp_headers":{}}`

	e := ParseLogLine(line)

	assert.False(t, e.ParseError)
	assert.Equal(t, "info", e.Level)
	assert.Equal(t, "http.log.access.log0", e.Logger)
	assert.Equal(t, "handled request", e.Message)
	assert.Equal(t, "example.com", e.Host)
	assert.Equal(t, "GET", e.Method)
	assert.Equal(t, "/api/users", e.URI)
	assert.Equal(t, 200, e.Status)
	assert.InDelta(t, 0.001234, e.Duration, 1e-9)
	assert.Equal(t, int64(1234), e.Size)
	assert.Equal(t, "192.168.1.1", e.RemoteIP)

	expected := time.Unix(1742472000, 123456000).UTC()
	assert.WithinDuration(t, expected, e.Timestamp, time.Microsecond)
}

func TestParseLogLine_RFC3339Timestamp(t *testing.T) {
	line := `{"level":"info","ts":"2026-04-13T10:15:30.5Z","logger":"http.log.access.log0","msg":"handled request","request":{"method":"POST","host":"api.example.com","uri":"/v1/items"},"status":201}`

	e := ParseLogLine(line)

	require.False(t, e.ParseError)
	assert.Equal(t, "api.example.com", e.Host)
	assert.Equal(t, 201, e.Status)
	expected, _ := time.Parse(time.RFC3339Nano, "2026-04-13T10:15:30.5Z")
	assert.True(t, e.Timestamp.Equal(expected), "timestamp mismatch: got %v want %v", e.Timestamp, expected)
}

func TestParseLogLine_MissingFields(t *testing.T) {
	line := `{"level":"info","msg":"something"}`

	e := ParseLogLine(line)

	assert.False(t, e.ParseError)
	assert.Equal(t, "info", e.Level)
	assert.Equal(t, "something", e.Message)
	assert.Empty(t, e.Host)
	assert.Empty(t, e.Method)
	assert.Equal(t, 0, e.Status)
	// Missing ts must fall back to a non-zero current time.
	assert.False(t, e.Timestamp.IsZero())
}

func TestParseLogLine_EmptyObject(t *testing.T) {
	e := ParseLogLine(`{}`)

	assert.False(t, e.ParseError)
	assert.False(t, e.Timestamp.IsZero())
}

func TestParseLogLine_InvalidJSON(t *testing.T) {
	line := `this is not json`

	e := ParseLogLine(line)

	assert.True(t, e.ParseError)
	assert.Equal(t, "this is not json", e.RawLine)
	assert.False(t, e.Timestamp.IsZero(), "parse errors must carry a fallback timestamp")
}

func TestParseLogLine_PartialJSON(t *testing.T) {
	line := `{"level":"info","msg":`

	e := ParseLogLine(line)

	assert.True(t, e.ParseError)
	assert.Equal(t, line, e.RawLine)
	assert.False(t, e.Timestamp.IsZero(), "parse errors must carry a fallback timestamp")
}

func TestParseLogLine_EmptyLine(t *testing.T) {
	for _, line := range []string{"", "   ", "\t\n"} {
		e := ParseLogLine(line)
		assert.True(t, e.ParseError, "line=%q", line)
		assert.False(t, e.Timestamp.IsZero(), "empty input still carries a fallback timestamp (line=%q)", line)
	}
}

func TestParseLogLine_UnknownFieldsIgnored(t *testing.T) {
	line := `{"level":"info","ts":1742472000,"custom_field":"value","nested":{"a":1},"request":{"method":"GET","host":"h","uri":"/"},"status":200}`

	e := ParseLogLine(line)

	assert.False(t, e.ParseError)
	assert.Equal(t, "h", e.Host)
	assert.Equal(t, 200, e.Status)
}

func TestParseLogLine_NullTimestamp(t *testing.T) {
	line := `{"level":"info","ts":null,"msg":"x"}`

	e := ParseLogLine(line)

	assert.False(t, e.ParseError)
	assert.False(t, e.Timestamp.IsZero())
}

func TestParseLogLine_UnparseableStringTimestamp(t *testing.T) {
	line := `{"level":"info","ts":"not-a-date","msg":"x","request":{"host":"h"},"status":200}`

	e := ParseLogLine(line)

	// Unknown string layout must not break parsing: the entry is kept with a
	// fallback timestamp so the user still sees the line.
	assert.False(t, e.ParseError)
	assert.Equal(t, "h", e.Host)
	assert.False(t, e.Timestamp.IsZero())
}

func TestIsAccessLog(t *testing.T) {
	tests := []struct {
		name   string
		entry  LogEntry
		expect bool
	}{
		{"server access logger", LogEntry{Logger: "http.log.access.log0"}, true},
		{"bare access prefix", LogEntry{Logger: "http.log.access"}, true},
		{"tls runtime logger", LogEntry{Logger: "tls.handshake"}, false},
		{"admin api logger", LogEntry{Logger: "admin.api"}, false},
		{"empty logger is runtime", LogEntry{Logger: ""}, false},
		{"parse error is never access", LogEntry{Logger: "http.log.access.log0", ParseError: true}, false},
		{"sibling logger not matching prefix", LogEntry{Logger: "http.log"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expect, tc.entry.IsAccessLog())
		})
	}
}

func BenchmarkParseLogLine(b *testing.B) {
	line := `{"level":"info","ts":1742472000.123456,"logger":"http.log.access.log0","msg":"handled request","request":{"remote_ip":"192.168.1.1","remote_port":"54321","proto":"HTTP/2.0","method":"GET","host":"example.com","uri":"/api/users"},"bytes_read":0,"duration":0.001234,"size":1234,"status":200,"resp_headers":{}}`

	b.ReportAllocs()
	for b.Loop() {
		_ = ParseLogLine(line)
	}
}
