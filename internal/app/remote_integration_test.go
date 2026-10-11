//go:build integration

package app

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Only a real Caddy checks the Host header the relay presents.
func TestIntegration_RemoteRelay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	direct := fetcher.NewHTTPFetcher(caddyAddr(), 0)
	before, err := direct.FetchConfig(ctx)
	require.NoError(t, err)

	pki := writeTestPKI(t)
	daemon, _ := startRemoteServer(t, &config{exposeCert: pki.serverCert, exposeKey: pki.serverKey, exposeCA: pki.caFile, relayed: direct})
	u, err := url.Parse(daemon)
	require.NoError(t, err)
	remote := fetcher.NewRemoteFetcher(u)
	require.NoError(t, configureTLS(remote.HTTPFetcher, fetcher.TLSOptions{CACert: pki.caFile, ClientCert: pki.clientCert, ClientKey: pki.clientKey}))
	t.Cleanup(remote.CloseIdleConnections)

	for range 20 {
		resp, err := http.Get("http://localhost:8080/")
		if err == nil {
			resp.Body.Close()
		}
	}
	remote.FetchServerNames(ctx)
	var state model.State
	for range 2 {
		snap, err := remote.Fetch(ctx)
		require.NoError(t, err)
		state.Update(snap)
		time.Sleep(300 * time.Millisecond)
	}
	assert.NotEmpty(t, state.HostDerived, "Caddy's metrics come through the relay")

	relayed, err := remote.FetchConfig(ctx)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(relayed), "the Caddy Config tab shows what an SSH session shows")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, daemon+"/caddy/load", nil)
	require.NoError(t, err)
	client := pki.client(t)
	t.Cleanup(client.CloseIdleConnections)
	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)

	after, err := direct.FetchConfig(ctx)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after), "a remote session leaves Caddy's config as it was")
}
