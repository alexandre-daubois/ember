package exporter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCertificatesHandler_DialsTheDaemonsHosts(t *testing.T) {
	holder := &StateHolder{}
	var s model.State
	s.Update(&fetcher.Snapshot{FetchedAt: time.Now(), Metrics: fetcher.MetricsSnapshot{
		Hosts: map[string]*fetcher.HostMetrics{"shop.test": {Host: "shop.test"}, "api.test": {Host: "api.test"}},
	}})
	holder.StoreAll(s.CopyForExport(), nil)
	var dialed []string
	dial := func(_ context.Context, hosts []string) []fetcher.CertificateInfo {
		dialed = hosts
		return []fetcher.CertificateInfo{{Subject: hosts[0], Source: "tls"}}
	}
	rec := httptest.NewRecorder()

	CertificatesHandler(holder, dial)(rec, httptest.NewRequest(http.MethodGet, "/certificates?host=evil.example", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var certs []fetcher.CertificateInfo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &certs))
	assert.Equal(t, "tls", certs[0].Source)
	assert.Equal(t, []string{"api.test", "shop.test"}, dialed, "only the hosts of the daemon's snapshot")
}
