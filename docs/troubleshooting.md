# Troubleshooting

Common issues and how to resolve them.

## Ember cannot connect to Caddy

**Symptom:** `Caddy UNREACHABLE | http://localhost:2019` or `connection refused` errors.

**Causes:**

- The Caddy admin API is disabled. Add `admin localhost:2019` to your Caddyfile global block.
- Caddy is listening on a different address. Use `--addr` to point Ember to the right endpoint.
- In Docker, Ember and Caddy are on different networks. Use `network_mode: "service:caddy"` or pass `--addr http://caddy:2019`.
- A firewall is blocking port 2019.

**Quick check:**

```bash
curl -s http://localhost:2019/config/ | head -c 100
```

If this returns JSON, the admin API is reachable. If not, fix Caddy's configuration first.

## No HTTP traffic metrics

**Symptom:** The Caddy tab shows hosts but RPS, latency, and status codes are all zero.

**Causes:**

- The `metrics` directive is missing from your Caddyfile. Add it to the global block:
  ```
  {
      admin localhost:2019
      metrics
  }
  ```
- Or run `ember init` to enable it via the admin API without restarting Caddy.
- No HTTP requests have been made yet. Metrics appear after the first request hits Caddy.

**Quick check:**

```bash
curl -s http://localhost:2019/metrics | grep caddy_http_requests_total
```

If no output, the metrics directive is not enabled.

## All traffic appears under a `*` host

**Symptom:** Instead of per-host rows, a single `*` row aggregates all traffic.

**Cause:** Caddy metrics lack per-host labels. This happens when your Caddyfile routes don't use host matchers.

**Fix:** Make sure your sites are defined with explicit hostnames:

```
example.com {
    respond "Hello"
}
```

Instead of:

```
:80 {
    respond "Hello"
}
```

Caddy only adds the `host` label to metrics when the route uses a host matcher.

## FrankenPHP tab does not appear

**Symptom:** Ember starts in Caddy-only mode even though FrankenPHP is running.

**Causes:**

- The `/frankenphp/threads` admin API endpoint is not available. This endpoint was added in FrankenPHP 1.4. Upgrade if you are on an older version.
- Ember checked before FrankenPHP was ready. Ember re-checks every 30 seconds, so the tab will appear once FrankenPHP becomes available.
- The admin API is disabled in your FrankenPHP configuration.

**Quick check:**

```bash
curl -s http://localhost:2019/frankenphp/threads | head -c 100
```

If this returns JSON with thread states, FrankenPHP is detectable.

## Thread metrics are empty (Method, URI, Mem, Reqs)

**Symptom:** The FrankenPHP tab shows threads but the Method, URI, Time, Mem, and Reqs columns are empty.

**Cause:** These metrics require FrankenPHP 1.13 or later. Older versions only expose thread index and state.

**Fix:** Upgrade FrankenPHP to 1.13+.

## CPU and RSS show 0%

**Symptom:** The process metrics (CPU, RSS) are stuck at zero.

**Causes:**

- Ember cannot find the Caddy/FrankenPHP process. This is common in containers where process scanning is restricted.
- Ember falls back to Prometheus `process_*` metrics automatically. If those are also missing, CPU and RSS stay at zero.

**Fix:** If running in a container, make sure `process_cpu_seconds_total` and `process_resident_memory_bytes` are present in Caddy's `/metrics` output. They are part of the default Go Prometheus collector and should be available unless explicitly disabled.

You can also pass `--frankenphp-pid` to skip process scanning:

```bash
ember --frankenphp-pid $(pgrep frankenphp)
```

## Latency percentiles are missing

**Symptom:** The host detail panel shows "no data" for P50/P90/P95/P99.

**Causes:**

- Percentiles are computed from Prometheus histogram buckets (`caddy_http_request_duration_seconds`). They require the `metrics` directive in the Caddyfile.
- Percentiles need two consecutive polls to compute a delta. On the first poll, they are unavailable.
- With `--json --once`, percentiles are always empty because there is no previous poll.

## Metrics endpoint returns 401 Unauthorized

**Symptom:** Prometheus scrapes fail with `401` after enabling `--metrics-auth`.

**Fix:** Add `basic_auth` to your Prometheus scrape configuration:

```yaml
scrape_configs:
  - job_name: ember
    basic_auth:
      username: admin
      password: secret
    static_configs:
      - targets: ["localhost:9191"]
```

## High memory usage in Ember

**Symptom:** Ember's own RSS is higher than expected.

**Context:** Ember targets ~15 MB RSS with 100 threads and 10 hosts. If you see significantly more:

- Check the number of unique hosts. Each host maintains its own metrics history.
- In graph mode, Ember stores 300 samples per metric. This is normal.
- Run `ember --json --once | wc -c` to check the size of a single snapshot. If it is very large, you may have an unusually high number of hosts or workers.

## See Also

- [Getting Started](getting-started.md)
- [Caddy Configuration](caddy-configuration.md)
- [CLI Reference](cli-reference.md)
