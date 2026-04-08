package fetcher

import "strings"

// IsUnixAddr reports whether addr represents a Unix socket address.
// It accepts both "unix//path" and "unix:///path" formats.
func IsUnixAddr(addr string) bool {
	return strings.HasPrefix(addr, "unix//") || strings.HasPrefix(addr, "unix:///")
}

// ParseUnixAddr extracts the socket file path from a Unix socket address.
// It returns the path and true if addr is a valid Unix socket address,
// or ("", false) otherwise.
func ParseUnixAddr(addr string) (string, bool) {
	var path string

	switch {
	case strings.HasPrefix(addr, "unix:///"):
		path = addr[len("unix:///"):]
	case strings.HasPrefix(addr, "unix//"):
		path = addr[len("unix//"):]
	default:
		return "", false
	}

	if path == "" {
		return "", false
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return path, true
}
