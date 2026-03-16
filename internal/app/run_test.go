package app

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidate_DaemonRequiresExpose(t *testing.T) {
	cfg := &config{daemon: true, expose: ""}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--daemon requires --expose")
}

func TestValidate_DaemonWithExposeOK(t *testing.T) {
	cfg := &config{daemon: true, expose: ":9191"}
	err := validate(cfg)
	assert.NoError(t, err)
}

func TestValidate_NoDaemonOK(t *testing.T) {
	cfg := &config{}
	assert.NoError(t, validate(cfg))
}

func TestRun_VersionFlag(t *testing.T) {
	cmd := newRootCmd("1.2.3-test")
	cmd.SetArgs([]string{"--version"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "ember 1.2.3-test")
}

func TestMetricsURL(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{":9191", "http://localhost:9191/metrics"},
		{"0.0.0.0:9191", "http://0.0.0.0:9191/metrics"},
		{"127.0.0.1:9191", "http://127.0.0.1:9191/metrics"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, metricsURL(tt.addr), "metricsURL(%q)", tt.addr)
	}
}

func TestRun_InvalidFlag(t *testing.T) {
	err := Run([]string{"--nonexistent"}, "0.0.0")
	assert.Error(t, err)
}

func TestRun_DaemonWithoutExpose(t *testing.T) {
	err := Run([]string{"--daemon"}, "0.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--daemon requires --expose")
}

func TestRun_CompletionBash(t *testing.T) {
	cmd := newRootCmd("0.0.0")
	cmd.SetArgs([]string{"completion", "bash"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "ember")
}

func TestRun_CompletionZsh(t *testing.T) {
	cmd := newRootCmd("0.0.0")
	cmd.SetArgs([]string{"completion", "zsh"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "ember")
}

func TestRun_CompletionFish(t *testing.T) {
	cmd := newRootCmd("0.0.0")
	cmd.SetArgs([]string{"completion", "fish"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "ember")
}

func TestRun_HelpContainsKeybindings(t *testing.T) {
	cmd := newRootCmd("1.0.0")
	cmd.SetArgs([]string{"--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	assert.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Keybindings")
	assert.Contains(t, out, "--addr")
	assert.Contains(t, out, "--expose")
	assert.Contains(t, out, "Examples")
}
