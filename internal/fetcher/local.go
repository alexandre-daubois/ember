package fetcher

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"syscall"

	psnet "github.com/shirou/gopsutil/v4/net"
)

// LocalListener describes a process listening on a local admin endpoint.
type LocalListener struct {
	PID       int32
	Ambiguous bool
}

// IsLocalAddr reports whether addrURL targets a process on the same host:
// HTTP(S) URLs whose host is a loopback name or address (localhost,
// 127.0.0.1, ::1), or any unix:// socket address.
func IsLocalAddr(addrURL string) bool {
	if IsUnixAddr(addrURL) {
		return true
	}
	u, err := url.Parse(addrURL)
	if err != nil {
		return false
	}
	return isLoopbackHost(u.Hostname())
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// FindLocalListener returns the process listening on the given local admin
// endpoint (a loopback HTTP(S) URL or a unix:// socket). It returns an empty
// LocalListener with an error when no listener can be matched, in which case
// callers should fall back to the Caddy-exposed process_* metrics.
func FindLocalListener(ctx context.Context, addrURL string) (LocalListener, error) {
	if path, ok := ParseUnixAddr(addrURL); ok {
		return findUnixListener(ctx, path)
	}
	u, err := url.Parse(addrURL)
	if err != nil {
		return LocalListener{}, fmt.Errorf("parse addr: %w", err)
	}
	if !isLoopbackHost(u.Hostname()) {
		return LocalListener{}, fmt.Errorf("addr %q is not local", addrURL)
	}
	portStr := u.Port()
	if portStr == "" {
		return LocalListener{}, fmt.Errorf("addr %q has no port", addrURL)
	}
	port, err := strconv.ParseUint(portStr, 10, 32)
	if err != nil {
		return LocalListener{}, fmt.Errorf("parse port %q: %w", portStr, err)
	}
	return findTCPListener(ctx, uint32(port))
}

func findTCPListener(ctx context.Context, port uint32) (LocalListener, error) {
	conns, err := psnet.ConnectionsWithContext(ctx, "tcp")
	if err != nil {
		return LocalListener{}, fmt.Errorf("list tcp connections: %w", err)
	}
	var first int32
	ambiguous := false
	for _, c := range conns {
		if c.Status != "LISTEN" || c.Laddr.Port != port || c.Pid <= 0 {
			continue
		}
		if !isLoopbackBind(c.Laddr.IP) {
			continue
		}
		if first == 0 {
			first = c.Pid
			continue
		}
		if c.Pid != first {
			ambiguous = true
		}
	}
	if first == 0 {
		return LocalListener{}, fmt.Errorf("no listener on port %d", port)
	}
	return LocalListener{PID: first, Ambiguous: ambiguous}, nil
}

func isLoopbackBind(ip string) bool {
	switch ip {
	case "", "127.0.0.1", "::1", "0.0.0.0", "::":
		return true
	}
	return strings.HasPrefix(ip, "127.")
}

func findUnixListener(ctx context.Context, path string) (LocalListener, error) {
	conns, err := psnet.ConnectionsWithContext(ctx, "unix")
	if err != nil {
		return LocalListener{}, fmt.Errorf("list unix connections: %w", err)
	}
	var first int32
	ambiguous := false
	for _, c := range conns {
		if c.Family != syscall.AF_UNIX || c.Laddr.IP != path || c.Pid <= 0 {
			continue
		}
		if first == 0 {
			first = c.Pid
			continue
		}
		if c.Pid != first {
			ambiguous = true
		}
	}
	if first == 0 {
		return LocalListener{}, fmt.Errorf("no listener on socket %q", path)
	}
	return LocalListener{PID: first, Ambiguous: ambiguous}, nil
}
