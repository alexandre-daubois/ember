package main

import (
	"testing"

	"github.com/alexandre-daubois/ember/internal/app"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersion(t *testing.T) {
	assert.NotEmpty(t, version)
}

func TestRun_Version(t *testing.T) {
	err := app.Run([]string{"--version"}, version)
	require.NoError(t, err)
}

func TestRun_Help(t *testing.T) {
	err := app.Run([]string{"--help"}, version)
	require.NoError(t, err)
}

func TestRun_InvalidFlag(t *testing.T) {
	err := app.Run([]string{"--nonexistent"}, version)
	assert.Error(t, err)
}

func TestRun_InvalidAddr(t *testing.T) {
	err := app.Run([]string{"--addr", "localhost:2019"}, version)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--addr must start with http:// or https://")
}

func TestRun_InvalidInterval(t *testing.T) {
	err := app.Run([]string{"--interval", "1ms"}, version)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--interval must be at least")
}

func TestRun_DaemonRequiresExpose(t *testing.T) {
	err := app.Run([]string{"--daemon"}, version)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--daemon requires --expose")
}

func TestRun_StatusHelp(t *testing.T) {
	err := app.Run([]string{"status", "--help"}, version)
	require.NoError(t, err)
}

func TestRun_WaitHelp(t *testing.T) {
	err := app.Run([]string{"wait", "--help"}, version)
	require.NoError(t, err)
}

func TestRun_InitHelp(t *testing.T) {
	err := app.Run([]string{"init", "--help"}, version)
	require.NoError(t, err)
}

func TestRun_DiffHelp(t *testing.T) {
	err := app.Run([]string{"diff", "--help"}, version)
	require.NoError(t, err)
}

func TestRun_VersionHelp(t *testing.T) {
	err := app.Run([]string{"version", "--help"}, version)
	require.NoError(t, err)
}
