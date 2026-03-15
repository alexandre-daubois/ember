package app

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := Run([]string{"--version"}, "1.2.3-test")

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()

	assert.NoError(t, runErr)
	assert.Contains(t, buf.String(), "ember 1.2.3-test")
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
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := Run([]string{"--completion", "bash"}, "0.0.0")

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()

	assert.NoError(t, runErr)
	assert.Contains(t, buf.String(), "complete -F _ember ember")
}

func TestRun_CompletionInvalid(t *testing.T) {
	err := Run([]string{"--completion", "powershell"}, "0.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported shell")
}

func TestPrintCompletion_AllShells(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		var buf bytes.Buffer
		err := printCompletion(&buf, shell)
		assert.NoError(t, err, shell)
		assert.Contains(t, buf.String(), "ember", shell)
	}
}

func TestPrintUsage_ContainsKey(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf, "1.0.0")
	out := buf.String()

	assert.Contains(t, out, "Ember 1.0.0")
	assert.Contains(t, out, "--addr")
	assert.Contains(t, out, "--expose")
	assert.Contains(t, out, "Keybindings")
	assert.Contains(t, out, "Examples")
}
