package fetcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsUnixAddr(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"unix//run/caddy/admin.sock", true},
		{"unix:///run/caddy/admin.sock", true},
		{"unix//tmp/test.sock", true},
		{"unix:///tmp/test.sock", true},
		{"http://localhost:2019", false},
		{"https://caddy:2019", false},
		{"localhost:2019", false},
		{"", false},
		{"unix", false},
		{"unix/", false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			assert.Equal(t, tt.want, IsUnixAddr(tt.addr))
		})
	}
}

func TestParseUnixAddr(t *testing.T) {
	tests := []struct {
		addr     string
		wantPath string
		wantOK   bool
	}{
		{"unix//run/caddy/admin.sock", "/run/caddy/admin.sock", true},
		{"unix:///run/caddy/admin.sock", "/run/caddy/admin.sock", true},
		{"unix//tmp/test.sock", "/tmp/test.sock", true},
		{"unix:///tmp/test.sock", "/tmp/test.sock", true},
		{"unix//relative/path.sock", "/relative/path.sock", true},
		{"unix///tmp/test.sock", "/tmp/test.sock", true},
		{"http://localhost:2019", "", false},
		{"https://caddy:2019", "", false},
		{"localhost:2019", "", false},
		{"", "", false},
		{"unix", "", false},
		{"unix/", "", false},
		{"unix//", "", false},
		{"unix:///", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			path, ok := ParseUnixAddr(tt.addr)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantPath, path)
		})
	}
}
