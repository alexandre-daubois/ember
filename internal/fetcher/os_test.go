package fetcher

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessHandle_DiscoveryCooldown(t *testing.T) {
	h := &processHandle{
		numCPU:            1,
		discoveryCooldown: 100 * time.Millisecond,
		lastDiscovery:     time.Now(),
	}

	metrics, err := h.fetch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ProcessMetrics{}, metrics)

	savedDiscovery := h.lastDiscovery

	metrics, err = h.fetch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ProcessMetrics{}, metrics)
	assert.Equal(t, savedDiscovery, h.lastDiscovery, "lastDiscovery should not change during cooldown")
}

func TestProcessHandle_DiscoveryAfterCooldown(t *testing.T) {
	h := &processHandle{
		numCPU:            1,
		discoveryCooldown: 50 * time.Millisecond,
		lastDiscovery:     time.Now().Add(-100 * time.Millisecond),
	}

	before := h.lastDiscovery

	_, err := h.fetch(context.Background())
	require.NoError(t, err)
	assert.True(t, h.lastDiscovery.After(before), "lastDiscovery should be updated after cooldown expires")
}

func TestProcessHandle_ResetSetsCooldown(t *testing.T) {
	h := &processHandle{
		numCPU:            1,
		discoveryCooldown: 100 * time.Millisecond,
	}

	before := time.Now()
	h.reset()
	assert.False(t, h.lastDiscovery.Before(before), "reset should set lastDiscovery to now")
	assert.Nil(t, h.proc)
	assert.Equal(t, float64(0), h.lastCPU)
	assert.True(t, h.lastSample.IsZero())
}
