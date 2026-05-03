package fetcher

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessHandle_NilProc(t *testing.T) {
	h := &processHandle{numCPU: 1}

	metrics, err := h.fetch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ProcessMetrics{}, metrics)
}

func TestProcessHandle_Reset(t *testing.T) {
	h := &processHandle{numCPU: 1}

	h.reset()
	assert.Nil(t, h.proc)
	assert.Zero(t, h.lastCPU)
	assert.True(t, h.lastSample.IsZero())
}
