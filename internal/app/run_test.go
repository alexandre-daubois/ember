package app

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/pkg/plugin"
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

func TestValidate_TimeoutBelowInterval(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, timeout: 200 * time.Millisecond, addr: "http://localhost:2019"}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--timeout")
}

func TestValidate_TimeoutEqualIntervalOK(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, timeout: 1 * time.Second, addr: "http://localhost:2019"}
	assert.NoError(t, validate(cfg))
}

func TestValidate_TimeoutZeroOK(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, timeout: 0, addr: "http://localhost:2019"}
	assert.NoError(t, validate(cfg))
}

func TestValidate_AddrMissingScheme(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "localhost:2019"}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--addr must start with http://, https://, or unix//")
}

func TestValidate_AddrHTTPSOK(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "https://caddy.internal:2019"}
	assert.NoError(t, validate(cfg))
}

func TestValidate_AddrHTTPOK(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "http://localhost:2019"}
	assert.NoError(t, validate(cfg))
}

func TestValidate_AddrUnixSocket(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "unix//run/caddy/admin.sock"}
	assert.NoError(t, validate(cfg))
}

func TestValidate_AddrUnixSocketTripleSlash(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "unix:///run/caddy/admin.sock"}
	assert.NoError(t, validate(cfg))
}

func TestValidate_AddrUnixSocketEmptyPath(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "unix//"}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "non-empty Unix socket path")
}

func TestValidate_AddrUnixSocketEmptyPathTripleSlash(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "unix:///"}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "non-empty Unix socket path")
}

func TestValidate_AddrUnixSocketWithTLS(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "unix//run/caddy/admin.sock", caCert: "ca.pem"}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TLS options cannot be used with Unix socket addresses")
}

func TestValidate_AddrUnixSocketWithClientCert(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "unix//run/caddy/admin.sock", clientCert: "cert.pem", clientKey: "key.pem"}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TLS options cannot be used with Unix socket addresses")
}

func TestValidate_AddrUnixSocketWithInsecure(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "unix//run/caddy/admin.sock", insecure: true}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TLS options cannot be used with Unix socket addresses")
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

func TestValidate_MetricsAuthColonOnly(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "http://localhost:2019", expose: ":9191", metricsAuth: ":"}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "both parts required")
}

func TestValidate_MetricsAuthEmptyUser(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "http://localhost:2019", expose: ":9191", metricsAuth: ":password"}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "both parts required")
}

func TestValidate_MetricsAuthEmptyPassword(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "http://localhost:2019", expose: ":9191", metricsAuth: "user:"}
	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "both parts required")
}

func TestValidate_MetricsAuthEmptyOK(t *testing.T) {
	cfg := &config{interval: 1 * time.Second, addr: "http://localhost:2019", expose: ":9191"}
	assert.NoError(t, validate(cfg))
}

func TestValidate_MetricsPrefix(t *testing.T) {
	cases := []struct {
		name    string
		prefix  string
		wantErr bool
	}{
		{"empty", "", false},
		{"snake_case", "my_app", false},
		{"single underscore", "_", false},
		{"leading underscore", "_app", false},
		{"alpha only", "myapp", false},
		{"alphanumeric", "app2", false},
		{"uppercase", "MyApp_42", false},
		{"kebab-case", "my-app", true},
		{"leading digit", "9app", true},
		{"colon (recording rule convention)", "team:app", true},
		{"dot", "team.app", true},
		{"space", "my app", true},
		{"unicode", "monApp\u00e9", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config{
				interval:      1 * time.Second,
				addr:          "http://localhost:2019",
				expose:        ":9191",
				metricsPrefix: tc.prefix,
			}
			err := validate(cfg)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "--metrics-prefix")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRun_IntervalTooLow(t *testing.T) {
	err := Run([]string{"--interval", "10ms"}, "0.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--interval must be at least")
}

func TestRun_AddrMissingScheme(t *testing.T) {
	err := Run([]string{"--addr", "localhost:2019"}, "0.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--addr must start with http://, https://, or unix//")
}

func TestRun_SubcommandValidatesAddr(t *testing.T) {
	err := Run([]string{"wait", "--addr", "localhost:2019"}, "0.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--addr must start with http://, https://, or unix//")
}

func TestRun_SubcommandValidatesInterval(t *testing.T) {
	err := Run([]string{"status", "--interval", "5ms"}, "0.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--interval must be at least")
}

func TestRun_DiffStillWorks(t *testing.T) {
	err := Run([]string{"diff", "--help"}, "0.0.0")
	assert.NoError(t, err)
}

func TestRun_VersionStillWorks(t *testing.T) {
	err := Run([]string{"version"}, "0.0.0")
	assert.NoError(t, err)
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

func TestNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	err := Run([]string{"--addr", "http://192.0.2.1:1", "--timeout", "1s", "status"}, "0.0.0")
	assert.Error(t, err)
	assert.NotContains(t, err.Error(), "NO_COLOR")
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

type testPlugin struct {
	name    string
	initCfg plugin.PluginConfig
	initErr error
}

func (p *testPlugin) Name() string { return p.name }
func (p *testPlugin) Init(_ context.Context, cfg plugin.PluginConfig) error {
	p.initCfg = cfg
	return p.initErr
}

func TestInitPlugins_Empty(t *testing.T) {
	plugin.Reset()
	cfg := &config{addr: "http://localhost:2019"}

	plugins, err := initPlugins(context.Background(), cfg)
	assert.NoError(t, err)
	assert.Nil(t, plugins)
}

func TestInitPlugins_Success(t *testing.T) {
	plugin.Reset()
	p := &testPlugin{name: "test"}
	plugin.Register(p)

	cfg := &config{addr: "http://localhost:2019"}
	plugins, err := initPlugins(context.Background(), cfg)

	assert.NoError(t, err)
	require.Len(t, plugins, 1)
	assert.Equal(t, "http://localhost:2019", p.initCfg.CaddyAddr)
}

func TestInitPlugins_InitError(t *testing.T) {
	plugin.Reset()
	plugin.Register(&testPlugin{name: "broken", initErr: assert.AnError})

	cfg := &config{addr: "http://localhost:2019"}
	_, err := initPlugins(context.Background(), cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin broken")
}

func TestPluginEnvOptions(t *testing.T) {
	t.Setenv("EMBER_PLUGIN_RATELIMIT_API_KEY", "abc123")
	t.Setenv("EMBER_PLUGIN_RATELIMIT_ENDPOINT", "http://localhost:8080")
	t.Setenv("EMBER_OTHER_VAR", "ignored")

	opts := pluginEnvOptions("ratelimit")

	assert.Equal(t, "abc123", opts["api_key"])
	assert.Equal(t, "http://localhost:8080", opts["endpoint"])
	assert.NotContains(t, opts, "other_var")
}

func TestPluginEnvOptions_HyphenatedName(t *testing.T) {
	t.Setenv("EMBER_PLUGIN_MY_PLUGIN_FOO", "bar")

	opts := pluginEnvOptions("my-plugin")
	assert.Equal(t, "bar", opts["foo"])
}

func TestPluginEnvOptions_Empty(t *testing.T) {
	opts := pluginEnvOptions("nonexistent")
	assert.Empty(t, opts)
}

func TestPluginEnvOptions_ValueWithEquals(t *testing.T) {
	t.Setenv("EMBER_PLUGIN_TEST_DSN", "postgres://user:pass@host/db?opt=val")

	opts := pluginEnvOptions("test")
	assert.Equal(t, "postgres://user:pass@host/db?opt=val", opts["dsn"])
}

func TestInitPlugins_PassesEnvOptions(t *testing.T) {
	plugin.Reset()
	t.Setenv("EMBER_PLUGIN_MYPLUGIN_KEY", "val")

	p := &testPlugin{name: "myplugin"}
	plugin.Register(p)

	cfg := &config{addr: "http://localhost:2019"}
	_, err := initPlugins(context.Background(), cfg)

	assert.NoError(t, err)
	assert.Equal(t, "val", p.initCfg.Options["key"])
}

type closableTestPlugin struct {
	testPlugin
	closed bool
}

type closableOrderPlugin struct {
	testPlugin
	closeFn func()
}

func (p *closableOrderPlugin) Close() error {
	p.closeFn()
	return nil
}

func (p *closableTestPlugin) Close() error {
	p.closed = true
	return nil
}

func TestInitPlugins_CleansUpOnFailure(t *testing.T) {
	plugin.Reset()

	good := &closableTestPlugin{testPlugin: testPlugin{name: "good"}}
	bad := &testPlugin{name: "bad", initErr: assert.AnError}

	plugin.Register(good)
	plugin.Register(bad)

	cfg := &config{addr: "http://localhost:2019"}
	_, err := initPlugins(context.Background(), cfg)

	require.Error(t, err)
	assert.True(t, good.closed, "successfully initialized plugin should be closed on failure")
}

func TestClosePlugins_SkipsNonCloser(t *testing.T) {
	p1 := &testPlugin{name: "first"}
	p2 := &testPlugin{name: "second"}

	assert.NotPanics(t, func() {
		closePlugins([]plugin.Plugin{p1, p2})
	})
}

func TestClosePlugins_ReverseOrder(t *testing.T) {
	var order []string

	p1 := &closableOrderPlugin{testPlugin: testPlugin{name: "first"}, closeFn: func() { order = append(order, "first") }}
	p2 := &closableOrderPlugin{testPlugin: testPlugin{name: "second"}, closeFn: func() { order = append(order, "second") }}

	closePlugins([]plugin.Plugin{p1, p2})
	assert.Equal(t, []string{"second", "first"}, order)
}

func TestClosePlugins_Empty(t *testing.T) {
	assert.NotPanics(t, func() {
		closePlugins(nil)
	})
}
