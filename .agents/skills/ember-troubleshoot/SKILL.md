---
name: ember-troubleshoot
description: "Diagnose and fix common Ember CLI issues. Use this skill whenever a user reports problems with ember: connection errors, missing metrics, empty dashboards, FrankenPHP tab not showing, CPU/RSS stuck at zero, missing latency percentiles, 401 errors on the metrics endpoint, high memory usage, or any 'ember is not working' situation. Also trigger when users see 'UNREACHABLE', 'connection refused', or describe metrics that are all zeros, even if they don't explicitly say 'troubleshoot'."
---

# Ember Troubleshooting

This skill helps diagnose and resolve common issues with Ember, the real-time Caddy/FrankenPHP terminal dashboard. Work through the relevant section based on the user's symptoms.

## Diagnostic approach

When a user reports a problem, start by identifying which symptom matches. If unclear, ask them to run:

```bash
ember status
```

This gives a one-line summary that reveals whether Caddy is reachable, metrics are flowing, and FrankenPHP is detected. If they get `Caddy UNREACHABLE`, start with the connection section. If they get `OK` but the dashboard looks wrong, move to the metrics sections.

---

## Ember cannot connect to Caddy

**Symptoms:** `Caddy UNREACHABLE | http://localhost:2019` or `connection refused`

**Diagnostic steps:**

1. Check if the Caddy admin API responds:
   ```bash
   curl -s http://localhost:2019/config/ | head -c 100
   ```

2. If no response, the issue is one of:
   - **Admin API disabled** — Add `admin localhost:2019` to the Caddyfile global block:
     ```
     {
         admin localhost:2019
         metrics
     }
     ```
   - **Different address** — Caddy might be on a non-default port. Check with `caddy environ` or inspect the Caddyfile, then use `--addr`:
     ```bash
     ember --addr http://your-host:2019
     ```
   - **Docker networking** — Ember and Caddy are on different networks. Fix with either:
     ```yaml
     # Option A: share network namespace
     network_mode: "service:caddy"

     # Option B: use service name
     # ember --addr http://caddy:2019
     ```
   - **Firewall** — Port 2019 is blocked. Check with `nc -z localhost 2019`.

---

## No HTTP traffic metrics (RPS, latency, status codes all zero)

**Symptoms:** The Caddy tab shows hosts but all numbers are zero.

**Diagnostic steps:**

1. Check if the metrics directive is enabled:
   ```bash
   curl -s http://localhost:2019/metrics | grep caddy_http_requests_total
   ```

2. If no output, the `metrics` directive is missing. Two ways to fix it:
   - **Quick fix** (no restart): run `ember init` — it enables metrics via the admin API automatically
   - **Permanent fix**: add `metrics` to the Caddyfile global block:
     ```
     {
         admin localhost:2019
         metrics
     }
     ```

3. If the grep returns results but numbers are still zero in Ember: no HTTP requests have been made yet. Metrics appear only after the first request hits Caddy. Try `curl http://localhost` to generate some traffic.

---

## All traffic under a single `*` host

**Symptoms:** Instead of per-host rows, a single `*` row aggregates everything.

**Cause:** Caddy only adds the `host` label to metrics when routes use host matchers.

**Fix:** Replace port-only site blocks with hostname-based blocks:

```
# Bad — no host label in metrics
:80 {
    respond "Hello"
}

# Good — per-host metrics
example.com {
    respond "Hello"
}
```

After changing the Caddyfile, reload Caddy (`caddy reload`). Historical metrics under `*` will age out as new per-host metrics come in.

---

## FrankenPHP tab does not appear

**Symptoms:** Ember starts in Caddy-only mode even though FrankenPHP is running.

**Diagnostic steps:**

1. Check the FrankenPHP admin endpoint:
   ```bash
   curl -s http://localhost:2019/frankenphp/threads | head -c 100
   ```

2. If no response:
   - **Old version** — The `/frankenphp/threads` endpoint was added in FrankenPHP 1.4. Upgrade if needed.
   - **Not ready yet** — Ember re-checks every 30 seconds. Wait a moment, or restart ember.
   - **Admin API disabled** in the FrankenPHP configuration.

3. If the endpoint returns JSON with thread states, FrankenPHP is detectable. Ember should show the tab within 30 seconds.

---

## Thread metrics are empty (Method, URI, Mem, Reqs columns)

**Symptoms:** FrankenPHP tab shows threads but the Method, URI, Time, Mem, and Reqs columns are blank.

**Cause:** These per-thread metrics require **FrankenPHP 1.13 or later**. Older versions only expose thread index and state.

**Fix:** Upgrade FrankenPHP to 1.13+.

---

## CPU and RSS stuck at 0%

**Symptoms:** Process metrics (CPU, RSS) never change from zero.

**Diagnostic steps:**

1. **In containers**, process scanning is restricted. Ember falls back to Prometheus `process_*` metrics. Check they exist:
   ```bash
   curl -s http://localhost:2019/metrics | grep process_cpu_seconds_total
   curl -s http://localhost:2019/metrics | grep process_resident_memory_bytes
   ```
   These are part of Go's default Prometheus collector and should be present unless explicitly disabled.

2. **Manual PID** — If process scanning fails, pass the PID directly:
   ```bash
   ember --frankenphp-pid $(pgrep frankenphp)
   ```

---

## Latency percentiles missing (P50/P90/P95/P99 show "no data")

**Symptoms:** The host detail panel shows "no data" for percentiles.

**Causes and fixes:**

1. **Missing metrics directive** — Percentiles come from `caddy_http_request_duration_seconds` histogram buckets, which require the `metrics` directive. Run `ember init` to enable it.

2. **First poll** — Percentiles need two consecutive polls to compute a delta. Wait a couple of seconds.

3. **Single snapshot mode** — `ember --json --once` always shows empty percentiles because there's no previous poll. Use `ember --json` (streaming) for percentiles.

---

## Metrics endpoint returns 401 Unauthorized

**Symptoms:** Prometheus scrapes fail with `401` after enabling `--metrics-auth`.

**Fix:** The Prometheus scrape config needs matching credentials:

```yaml
scrape_configs:
  - job_name: ember
    basic_auth:
      username: admin
      password: secret
    static_configs:
      - targets: ["localhost:9191"]
```

Make sure the `username:password` matches what was passed to `--metrics-auth`.

---

## High memory usage

**Symptoms:** Ember's RSS is higher than expected (~15 MB is normal with 100 threads and 10 hosts).

**Diagnostic steps:**

1. Check the number of unique hosts — each host maintains its own metrics history.
2. In graph mode, Ember stores 300 samples per metric. This is normal.
3. Measure a snapshot size:
   ```bash
   ember --json --once | wc -c
   ```
   If very large, there may be an unusually high number of hosts or workers.

---

## General diagnostic checklist

When none of the above fits, walk through this checklist:

1. `ember version --check` — Is Ember up to date?
2. `ember status` — Can Ember reach Caddy at all?
3. `ember init` — Does the validation pass all checks?
4. `curl http://localhost:2019/config/` — Is the admin API responding?
5. `curl http://localhost:2019/metrics | head -20` — Are metrics being generated?
6. Check the Caddyfile for `admin` and `metrics` directives in the global block.
