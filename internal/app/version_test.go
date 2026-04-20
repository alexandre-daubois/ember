package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionCmd_PrintsVersion(t *testing.T) {
	cmd := newRootCmd("1.2.3")
	cmd.SetArgs([]string{"version"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "ember 1.2.3")
}

func TestCheckLatestVersion_UpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(githubRelease{TagName: "v1.2.3", HTMLURL: "https://github.com/test/releases/v1.2.3"})
	}))
	defer srv.Close()

	origURL := latestReleaseURL
	setLatestReleaseURL(srv.URL)
	defer setLatestReleaseURL(origURL)

	var buf bytes.Buffer
	err := checkLatestVersion(context.Background(), &buf, "1.2.3")

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "latest version")
}

func TestCheckLatestVersion_NewerAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(githubRelease{TagName: "v2.0.0", HTMLURL: "https://github.com/test/releases/v2.0.0"})
	}))
	defer srv.Close()

	origURL := latestReleaseURL
	setLatestReleaseURL(srv.URL)
	defer setLatestReleaseURL(origURL)

	var buf bytes.Buffer
	err := checkLatestVersion(context.Background(), &buf, "1.2.3")

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "newer version")
	assert.Contains(t, buf.String(), "v2.0.0")
}

func TestCheckLatestVersion_DevBuild(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(githubRelease{TagName: "v1.0.0", HTMLURL: "https://github.com/test/releases/v1.0.0"})
	}))
	defer srv.Close()

	origURL := latestReleaseURL
	setLatestReleaseURL(srv.URL)
	defer setLatestReleaseURL(origURL)

	var buf bytes.Buffer
	err := checkLatestVersion(context.Background(), &buf, "1.0.0-dev")

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Development build")
}

func TestCheckLatestVersion_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	origURL := latestReleaseURL
	setLatestReleaseURL(srv.URL)
	defer setLatestReleaseURL(origURL)

	var buf bytes.Buffer
	err := checkLatestVersion(context.Background(), &buf, "1.0.0")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not check")
}

func TestCheckLatestVersion_WithVPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(githubRelease{TagName: "v1.2.3", HTMLURL: "https://example.com"})
	}))
	defer srv.Close()

	origURL := latestReleaseURL
	setLatestReleaseURL(srv.URL)
	defer setLatestReleaseURL(origURL)

	var buf bytes.Buffer
	err := checkLatestVersion(context.Background(), &buf, "v1.2.3")

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "latest version")
}

func TestVersionCmd_CheckFlag_HitsAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v1.2.3", HTMLURL: "https://example/release"})
	}))
	defer srv.Close()

	origURL := latestReleaseURL
	setLatestReleaseURL(srv.URL)
	defer setLatestReleaseURL(origURL)

	cmd := newRootCmd("1.2.3")
	cmd.SetArgs([]string{"version", "--check"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())
	out := buf.String()
	assert.Contains(t, out, "ember 1.2.3")
	assert.Contains(t, out, "latest version",
		"--check must consult the release API and report the result")
}

func TestVersionCmd_Help(t *testing.T) {
	cmd := newRootCmd("1.0.0")
	cmd.SetArgs([]string{"version", "--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "--check")
}
