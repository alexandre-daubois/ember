package fetcher

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsLocalAddr(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"http://localhost:2019", true},
		{"http://LOCALHOST:2019", true},
		{"http://127.0.0.1:2019", true},
		{"http://[::1]:2019", true},
		{"https://localhost:2019", true},
		{"unix//var/run/caddy.sock", true},
		{"unix:///var/run/caddy.sock", true},
		{"http://prod.example.com:2019", false},
		{"http://10.0.0.1:2019", false},
		{"http://[2001:db8::1]:2019", false},
		{"", false},
		{"::not a url::", false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			assert.Equal(t, tt.want, IsLocalAddr(tt.addr))
		})
	}
}

func TestFindLocalListener_NotLocal(t *testing.T) {
	_, err := FindLocalListener(context.Background(), "http://prod.example.com:2019")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not local")
}

func TestFindLocalListener_BadURL(t *testing.T) {
	_, err := FindLocalListener(context.Background(), "http://localhost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no port")
}

func TestFindLocalListener_BadPort(t *testing.T) {
	_, err := FindLocalListener(context.Background(), "http://localhost:abc")
	require.Error(t, err)
}

func TestFindLocalListener_TCPMatchesCurrentProcess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	port := ln.Addr().(*net.TCPAddr).Port
	addr := "http://127.0.0.1:" + strconv.Itoa(port)

	ll, err := FindLocalListener(context.Background(), addr)
	if err != nil {
		t.Skipf("connections lookup unavailable in this env: %v", err)
	}
	assert.Equal(t, int32(os.Getpid()), ll.PID)
	assert.False(t, ll.Ambiguous)
}

func TestFindLocalListener_TCPNoListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())

	addr := "http://127.0.0.1:" + strconv.Itoa(port)
	_, err = FindLocalListener(context.Background(), addr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no listener")
}

func TestFindLocalListener_Unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets not supported on Windows")
	}
	dir := t.TempDir()
	sock := filepath.Join(dir, "ember.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	ll, err := FindLocalListener(context.Background(), "unix//"+strings.TrimPrefix(sock, "/"))
	if err != nil {
		t.Skipf("unix connections lookup unavailable: %v", err)
	}
	assert.Equal(t, int32(os.Getpid()), ll.PID)
}

func TestIsLoopbackHost(t *testing.T) {
	assert.True(t, isLoopbackHost("localhost"))
	assert.True(t, isLoopbackHost("LOCALHOST"))
	assert.True(t, isLoopbackHost("127.0.0.1"))
	assert.True(t, isLoopbackHost("::1"))
	assert.False(t, isLoopbackHost("example.com"))
	assert.False(t, isLoopbackHost("10.0.0.1"))
	assert.False(t, isLoopbackHost(""))
}

func TestIsLoopbackBind(t *testing.T) {
	assert.True(t, isLoopbackBind(""))
	assert.True(t, isLoopbackBind("127.0.0.1"))
	assert.True(t, isLoopbackBind("127.0.1.1"))
	assert.True(t, isLoopbackBind("::1"))
	assert.True(t, isLoopbackBind("0.0.0.0"))
	assert.True(t, isLoopbackBind("::"))
	assert.False(t, isLoopbackBind("10.0.0.1"))
	assert.False(t, isLoopbackBind("1.2.3.4"))
}
