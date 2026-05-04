//go:build integration_docker

package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_MultiCaddySmoke exercises the documented multi-instance flow
// against the local/multi-caddy/ docker-compose stack: it brings up two real
// Caddy instances, runs the ember binary in --daemon mode pointing at both,
// and asserts that /metrics exposes ember_instance="blue" and ="green".
func TestIntegration_MultiCaddySmoke(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker binary not found in PATH; skipping multi-caddy smoke test")
	}

	composeFile, repoRoot := composePaths(t)

	dockerCompose := func(ctx context.Context, args ...string) *exec.Cmd {
		full := append([]string{"compose", "-f", composeFile}, args...)
		c := exec.CommandContext(ctx, "docker", full...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c
	}

	preCtx, preCancel := context.WithTimeout(context.Background(), 30*time.Second)
	_ = dockerCompose(preCtx, "down", "-v", "--remove-orphans").Run()
	preCancel()

	upCtx, upCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer upCancel()
	require.NoError(t, dockerCompose(upCtx, "up", "-d").Run(), "docker compose up failed")

	t.Cleanup(func() {
		downCtx, downCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer downCancel()
		_ = dockerCompose(downCtx, "down", "-v", "--remove-orphans").Run()
	})

	waitHTTP(t, "http://localhost:2019/config/", 30*time.Second)
	waitHTTP(t, "http://localhost:2020/config/", 30*time.Second)

	expose := freePortAddr(t)

	emberCtx, emberCancel := context.WithCancel(context.Background())
	defer emberCancel()

	ember := exec.CommandContext(emberCtx, "go", "run", "./cmd/ember",
		"--daemon",
		"--expose", expose,
		"--addr", "blue=http://localhost:2019",
		"--addr", "green=http://localhost:2020",
	)
	ember.Dir = repoRoot
	ember.Cancel = func() error { return ember.Process.Signal(syscall.SIGTERM) }
	ember.WaitDelay = 5 * time.Second
	var emberErr strings.Builder
	ember.Stdout = io.Discard
	ember.Stderr = &emberErr
	require.NoError(t, ember.Start(), "ember subprocess failed to start")

	t.Cleanup(func() {
		emberCancel()
		_ = ember.Wait()
		if t.Failed() {
			t.Logf("ember stderr:\n%s", emberErr.String())
		}
	})

	url := "http://" + expose
	waitHTTP(t, url+"/healthz", 30*time.Second)

	body := scrapeMetricsUntil(t, url+"/metrics", 30*time.Second, func(b string) bool {
		return strings.Contains(b, `ember_instance="blue"`) &&
			strings.Contains(b, `ember_instance="green"`)
	})

	assert.Contains(t, body, `ember_instance="blue"`)
	assert.Contains(t, body, `ember_instance="green"`)
}

func composePaths(t *testing.T) (composeFile, repoRoot string) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	repoRoot = filepath.Dir(filepath.Dir(filepath.Dir(file)))
	composeFile = filepath.Join(repoRoot, "local", "multi-caddy", "compose.yaml")
	return composeFile, repoRoot
}

func freePortAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

func waitHTTP(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("server %s not ready within %s", url, timeout)
}

func scrapeMetricsUntil(t *testing.T, url string, deadline time.Duration, predicate func(string) bool) string {
	t.Helper()
	end := time.Now().Add(deadline)
	var last string
	for time.Now().Before(end) {
		resp, err := http.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			last = string(body)
			if resp.StatusCode == http.StatusOK && predicate(last) {
				return last
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("predicate not satisfied within %s; last body:\n%s", deadline, last)
	return last
}
