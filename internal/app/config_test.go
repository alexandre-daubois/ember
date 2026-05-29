package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetDefaultKey_ReplaceExisting(t *testing.T) {
	in := "default = \"production\"\n\n[[endpoints]]\nname = \"production\"\n"
	out := setDefaultKey(in, "staging")
	assert.Contains(t, out, "default = \"staging\"")
	assert.NotContains(t, out, "\"production\"\n\n[[endpoints]]")
}

func TestSetDefaultKey_PreservesTrailingComment(t *testing.T) {
	in := "default = \"production\"   # optional, TUI mode only\n[[endpoints]]\n"
	out := setDefaultKey(in, "staging")
	assert.Contains(t, out, "default = \"staging\"   # optional, TUI mode only")
}

func TestSetDefaultKey_InsertWhenAbsent(t *testing.T) {
	in := "[[endpoints]]\nname = \"web1\"\naddr = \"http://a\"\n"
	out := setDefaultKey(in, "web1")
	assert.Greater(t, len(out), len(in))
	assert.Contains(t, out, "default = \"web1\"")
	// the inserted key sits before the first table header
	assert.Less(t, indexOf(out, "default ="), indexOf(out, "[[endpoints]]"))
}

func TestSetDefaultKey_IgnoresKeyInsideTable(t *testing.T) {
	// a "default" token appearing after a table header must not be mistaken
	// for the top-level key; a fresh one is prepended instead.
	in := "[[endpoints]]\nname = \"default\"\naddr = \"http://a\"\n"
	out := setDefaultKey(in, "default")
	assert.Less(t, indexOf(out, "default ="), indexOf(out, "[[endpoints]]"))
}

func indexOf(s, sub string) int {
	return bytes.Index([]byte(s), []byte(sub))
}

func writeUseFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".ember.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestRunConfigUse_SetsExisting(t *testing.T) {
	file := writeUseFile(t, "default = \"web1\"\n\n[[endpoints]]\nname = \"web1\"\naddr = \"http://a\"\n\n[[endpoints]]\nname = \"web2\"\naddr = \"http://b\"\n")
	var buf bytes.Buffer
	require.NoError(t, runConfigUse(&buf, file, "web2"))
	assert.Contains(t, buf.String(), "web2")

	fc, err := parseConfigFile(file)
	require.NoError(t, err)
	assert.Equal(t, "web2", fc.Default)
}

func TestRunConfigUse_InsertsWhenAbsent(t *testing.T) {
	file := writeUseFile(t, "[[endpoints]]\nname = \"web1\"\naddr = \"http://a\"\n")
	var buf bytes.Buffer
	require.NoError(t, runConfigUse(&buf, file, "web1"))

	fc, err := parseConfigFile(file)
	require.NoError(t, err)
	assert.Equal(t, "web1", fc.Default)
	require.Len(t, fc.Endpoints, 1, "endpoints must survive the edit")
}

func TestRunConfigUse_PreservesComment(t *testing.T) {
	file := writeUseFile(t, "default = \"web1\" # keep me\n[[endpoints]]\nname = \"web1\"\naddr = \"http://a\"\n[[endpoints]]\nname = \"web2\"\naddr = \"http://b\"\n")
	var buf bytes.Buffer
	require.NoError(t, runConfigUse(&buf, file, "web2"))

	data, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(data), "# keep me")
	assert.Contains(t, string(data), "default = \"web2\"")
}

func TestRunConfigUse_UnknownName(t *testing.T) {
	file := writeUseFile(t, "[[endpoints]]\nname = \"web1\"\naddr = \"http://a\"\n")
	original, err := os.ReadFile(file)
	require.NoError(t, err)

	err = runConfigUse(&bytes.Buffer{}, file, "ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	after, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, original, after, "file must be untouched on error")
}

func TestRunConfigUse_MissingFile(t *testing.T) {
	err := runConfigUse(&bytes.Buffer{}, filepath.Join(t.TempDir(), "absent.toml"), "web1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no config file")
}

func TestRunConfigUse_Malformed(t *testing.T) {
	file := writeUseFile(t, "not = valid [[[")
	err := runConfigUse(&bytes.Buffer{}, file, "web1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config file")
}

func TestRun_ConfigUse_EndToEnd(t *testing.T) {
	file := writeUseFile(t, "[[endpoints]]\nname = \"web1\"\naddr = \"http://a\"\n[[endpoints]]\nname = \"web2\"\naddr = \"http://b\"\n")
	require.NoError(t, Run([]string{"-f", file, "config", "use", "web2"}, "0.0.0"))

	fc, err := parseConfigFile(file)
	require.NoError(t, err)
	assert.Equal(t, "web2", fc.Default)
}

func TestRun_ConfigUse_RequiresName(t *testing.T) {
	err := Run([]string{"config", "use"}, "0.0.0")
	require.Error(t, err, "config use requires exactly one argument")
}
