package app

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, nil))
}

func TestErrorThrottle_FirstErrorLogged(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	var et errorThrottle
	et.record(log, assert.AnError)

	assert.Contains(t, buf.String(), "fetch failed")
	assert.True(t, et.failing)
}

func TestErrorThrottle_SubsequentErrorsSuppressed(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	var et errorThrottle
	et.record(log, assert.AnError)
	buf.Reset()

	et.record(log, assert.AnError)
	et.record(log, assert.AnError)

	assert.Empty(t, buf.String(), "repeated errors within interval should be suppressed")
	assert.Equal(t, 2, et.suppressed)
}

func TestErrorThrottle_LogsAfterInterval(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	var et errorThrottle
	et.record(log, assert.AnError)
	buf.Reset()

	et.suppressed = 5
	et.lastLogged = time.Now().Add(-errorThrottleInterval - time.Second)

	et.record(log, assert.AnError)

	require.Contains(t, buf.String(), "fetch failed")
	assert.Contains(t, buf.String(), "suppressed=5")
	assert.Equal(t, 0, et.suppressed)
}

func TestErrorThrottle_RecoverLogs(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	var et errorThrottle
	et.record(log, assert.AnError)
	buf.Reset()

	et.recover(log)

	assert.Contains(t, buf.String(), "fetch recovered")
	assert.False(t, et.failing)
}

func TestErrorThrottle_RecoverNoopWhenNotFailing(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	var et errorThrottle
	et.recover(log)

	assert.Empty(t, buf.String(), "recover should not log when not failing")
}
