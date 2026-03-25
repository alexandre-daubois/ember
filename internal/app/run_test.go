package app

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestValidate_DaemonRequiresExpose(t *testing.T) {
	cfg := &config{daemon: true, expose: ""}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--daemon requires --expose")
}

func TestValidate_DaemonWithExposeOK(t *testing.T) {
	cfg := &config{daemon: true, expose: ":9191", interval: 1 * time.Second, addr: "http://localhost:2019"}
	err := validate(cfg)
	assert.NoError(t, err)
}

func TestValidate_NoDaemonOK(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "http://localhost:2019"}
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

func TestValidate_OnceRequiresJSON(t *testing.T) {
	cfg := &config{once: true, jsonMode: false}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--once requires --json")
}

func TestValidate_OnceWithDaemon(t *testing.T) {
	cfg := &config{once: true, jsonMode: true, daemon: true, expose: ":9191"}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--once is incompatible with --daemon")
}

func TestValidate_OnceWithJSONOK(t *testing.T) {
	cfg := &config{once: true, jsonMode: true, interval: 1 * time.Second, addr: "http://localhost:2019"}
	assert.NoError(t, validate(cfg))
}

func TestRun_OnceWithoutJSON(t *testing.T) {
	err := Run([]string{"--once"}, "0.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--once requires --json")
}

func TestValidate_IntervalTooLow(t *testing.T) {
	cfg := &config{interval: 10 * time.Millisecond, addr: "http://localhost:2019"}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--interval must be at least")
}

func TestValidate_IntervalAtMinimumOK(t *testing.T) {
	cfg := &config{interval: 100 * time.Millisecond, addr: "http://localhost:2019"}
	assert.NoError(t, validate(cfg))
}

func TestValidate_IntervalAboveMinimumOK(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "http://localhost:2019"}
	assert.NoError(t, validate(cfg))
}

func TestValidate_AddrMissingScheme(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "localhost:2019"}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--addr must start with http:// or https://")
}

func TestValidate_AddrHTTPSOK(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "https://caddy.internal:2019"}
	assert.NoError(t, validate(cfg))
}

func TestValidate_AddrHTTPOK(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "http://localhost:2019"}
	assert.NoError(t, validate(cfg))
}

func TestValidate_MetricsAuthBadFormat(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "http://localhost:2019", expose: ":9191", metricsAuth: "nopassword"}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user:password format")
}

func TestValidate_MetricsAuthRequiresExpose(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "http://localhost:2019", metricsAuth: "user:pass"}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--metrics-auth requires --expose")
}

func TestValidate_MetricsAuthOK(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "http://localhost:2019", expose: ":9191", metricsAuth: "admin:secret"}
	assert.NoError(t, validate(cfg))
}

func TestValidate_MetricsAuthEmptyOK(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "http://localhost:2019", expose: ":9191"}
	assert.NoError(t, validate(cfg))
}

func TestRun_IntervalTooLow(t *testing.T) {
	err := Run([]string{"--interval", "10ms"}, "0.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--interval must be at least")
}

func TestRun_AddrMissingScheme(t *testing.T) {
	err := Run([]string{"--addr", "localhost:2019"}, "0.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--addr must start with http:// or https://")
}

func TestContextWithTimeout_Zero(t *testing.T) {
	parent := context.Background()
	ctx, cancel := contextWithTimeout(parent, 0)
	defer cancel()

	_, hasDeadline := ctx.Deadline()
	assert.False(t, hasDeadline, "zero timeout should not set a deadline")
}

func TestContextWithTimeout_NonZero(t *testing.T) {
	parent := context.Background()
	ctx, cancel := contextWithTimeout(parent, 5*time.Second)
	defer cancel()

	deadline, hasDeadline := ctx.Deadline()
	assert.True(t, hasDeadline)
	assert.True(t, deadline.After(time.Now()))
}

func TestRun_TimeoutFlagAvailable(t *testing.T) {
	cmd := newRootCmd("1.0.0")
	cmd.SetArgs([]string{"--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "--timeout")
}

func TestRun_TimeoutInheritedBySubcommands(t *testing.T) {
	cmd := newRootCmd("1.0.0")
	cmd.SetArgs([]string{"wait", "--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "--timeout")
}

func TestInitLogger_Text(t *testing.T) {
	cfg := &config{logFormat: "text"}
	initLogger(cfg)
	assert.NotNil(t, cfg.logger)
}

func TestInitLogger_JSON(t *testing.T) {
	cfg := &config{logFormat: "json"}
	initLogger(cfg)
	assert.NotNil(t, cfg.logger)
}

func TestInitLogger_DefaultIsText(t *testing.T) {
	cfg := &config{}
	initLogger(cfg)
	assert.NotNil(t, cfg.logger)
}

func TestRun_LogFormatFlagAvailable(t *testing.T) {
	cmd := newRootCmd("1.0.0")
	cmd.SetArgs([]string{"--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "--log-format")
}

func TestBindEnv_AddrFromEnv(t *testing.T) {
	t.Setenv("EMBER_ADDR", "http://remote:2019")

	cmd := newRootCmd("0.0.0")
	bindEnv(cmd)

	assert.Equal(t, "http://remote:2019", cmd.Flag("addr").Value.String())
}

func TestBindEnv_FlagOverridesEnv(t *testing.T) {
	t.Setenv("EMBER_ADDR", "http://env:2019")

	cmd := newRootCmd("0.0.0")
	cmd.Flag("addr").Value.Set("http://flag:2019")
	cmd.Flag("addr").Changed = true
	bindEnv(cmd)

	assert.Equal(t, "http://flag:2019", cmd.Flag("addr").Value.String())
}

func TestBindEnv_IntervalFromEnv(t *testing.T) {
	t.Setenv("EMBER_INTERVAL", "5s")

	cmd := newRootCmd("0.0.0")
	bindEnv(cmd)

	assert.Equal(t, "5s", cmd.Flag("interval").Value.String())
}

func TestBindEnv_ExposeFromEnv(t *testing.T) {
	t.Setenv("EMBER_EXPOSE", ":9191")

	cmd := newRootCmd("0.0.0")
	bindEnv(cmd)

	assert.Equal(t, ":9191", cmd.Flag("expose").Value.String())
}

func TestBindEnv_MetricsPrefixFromEnv(t *testing.T) {
	t.Setenv("EMBER_METRICS_PREFIX", "myapp")

	cmd := newRootCmd("0.0.0")
	bindEnv(cmd)

	assert.Equal(t, "myapp", cmd.Flag("metrics-prefix").Value.String())
}

func TestBindEnv_MetricsAuthFromEnv(t *testing.T) {
	t.Setenv("EMBER_METRICS_AUTH", "admin:secret")

	cmd := newRootCmd("0.0.0")
	bindEnv(cmd)

	assert.Equal(t, "admin:secret", cmd.Flag("metrics-auth").Value.String())
}

func TestBindEnv_UnsetEnvKeepsDefault(t *testing.T) {
	cmd := newRootCmd("0.0.0")
	bindEnv(cmd)

	assert.Equal(t, "http://localhost:2019", cmd.Flag("addr").Value.String())
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
