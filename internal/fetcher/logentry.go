package fetcher

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// LogEntry is a single access log line parsed from Caddy's JSON log output.
// RawLine is populated when the input is not valid JSON so the UI can still
// show the line to the user.
type LogEntry struct {
	Timestamp  time.Time
	Level      string
	Logger     string
	Message    string
	Host       string
	Method     string
	URI        string
	Status     int
	Duration   float64
	Size       int64
	RemoteIP   string
	RawLine    string
	ParseError bool
}

// caddyLogLine mirrors the JSON shape Caddy emits for access logs.
// Unknown fields are ignored.
type caddyLogLine struct {
	Timestamp logTimestamp `json:"ts"`
	Level     string       `json:"level"`
	Logger    string       `json:"logger"`
	Message   string       `json:"msg"`
	Request   caddyLogReq  `json:"request"`
	Status    int          `json:"status"`
	Duration  float64      `json:"duration"`
	Size      int64        `json:"size"`
}

type caddyLogReq struct {
	RemoteIP string `json:"remote_ip"`
	Method   string `json:"method"`
	Host     string `json:"host"`
	URI      string `json:"uri"`
}

// logTimestamp accepts both a float Unix timestamp (Caddy's default
// "unix_seconds_float" formatter) and an RFC3339 / ISO-8601 string.
type logTimestamp struct {
	time.Time
}

func (t *logTimestamp) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		unquoted, err := strconv.Unquote(string(data))
		if err != nil {
			return err
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, unquoted); err == nil {
				t.Time = parsed
				return nil
			}
		}
		// Unknown string layout: silently leave zero, caller may fall back.
		return nil
	}
	f, err := strconv.ParseFloat(string(data), 64)
	if err != nil {
		return err
	}
	sec := int64(f)
	nsec := int64((f - float64(sec)) * 1e9)
	t.Time = time.Unix(sec, nsec).UTC()
	return nil
}

// ParseLogLine decodes a single JSON log line into a LogEntry. A line that is
// not valid JSON is returned with ParseError=true and RawLine populated so the
// caller can still show it in context.
//
// Timestamp is always non-zero: on a parse error we fall back to time.Now
// so callers that sort or timestamp-format the entry do not see "0001-01-01".
func ParseLogLine(line string) LogEntry {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return LogEntry{ParseError: true, RawLine: line, Timestamp: time.Now().UTC()}
	}

	var parsed caddyLogLine
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return LogEntry{ParseError: true, RawLine: trimmed, Timestamp: time.Now().UTC()}
	}

	ts := parsed.Timestamp.Time
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	return LogEntry{
		Timestamp: ts,
		Level:     parsed.Level,
		Logger:    parsed.Logger,
		Message:   parsed.Message,
		Host:      parsed.Request.Host,
		Method:    parsed.Request.Method,
		URI:       parsed.Request.URI,
		Status:    parsed.Status,
		Duration:  parsed.Duration,
		Size:      parsed.Size,
		RemoteIP:  parsed.Request.RemoteIP,
	}
}
