# Ember - Real-time terminal dashboard for FrankenPHP

Understand why your app is on fire by monitoring threads, workers, memory, request rates and more.

![ember screenshot](https://github.com/alexandre-daubois/ember/blob/main/assets/img.png?raw=true)

## Install

```bash
go install github.com/alexandredaubois/ember/cmd/ember@latest
```

## Usage

Make sure FrankenPHP is running with the admin API enabled:

```
{
    admin localhost:2019
    metrics
}
```

Then:

```bash
ember
```

Ember auto-detects the FrankenPHP process and connects to the Caddy admin API.

### Options

```
--addr string    Caddy admin API address (default "http://localhost:2019")
--interval dur   Polling interval (default 1s)
--pid int        FrankenPHP PID (auto-detected if not set)
--json           JSON output mode (for scripting)
--no-color       Disable colors
```

### Keybindings

| Key | Action |
|-----|--------|
| `↑` `↓` | Navigate threads |
| `Enter` | Thread detail panel |
| `s` / `S` | Cycle sort field |
| `p` | Pause / resume |
| `l` | Toggle leak watcher |
| `r` | Restart workers |
| `/` | Filter threads |
| `q` | Quit |

## License

MIT
