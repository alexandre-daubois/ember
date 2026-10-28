package exporter

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsHandler_RejectsABadCursor(t *testing.T) {
	h := LogsHandler(nil)
	for _, query := range []string{"", "after=abc", "after=-2"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/logs?"+query, nil))
		assert.Equal(t, http.StatusBadRequest, rec.Code, query)
	}
}

type pageSource struct{}

func (pageSource) Epoch() int64 { return 7 }

func (pageSource) Since(int64, int) ([]fetcher.LogEntry, int64, error) {
	return []fetcher.LogEntry{{Message: "hello"}}, 1, nil
}

// TestLogsHandler_GzipOnlyWhenTheClientAcceptsIt pins that a page is gzipped
// for a client that accepts it and sent plain otherwise.
func TestLogsHandler_GzipOnlyWhenTheClientAcceptsIt(t *testing.T) {
	for acceptEncoding, gzipped := range map[string]bool{"gzip": true, "": false, "gzip;q=0": false} {
		t.Run(acceptEncoding, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/logs?after=0", nil)
			req.Header.Set("Accept-Encoding", acceptEncoding)
			rec := httptest.NewRecorder()
			LogsHandler(pageSource{}).ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			var body io.Reader = rec.Body
			if gzipped {
				assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
				gz, err := gzip.NewReader(rec.Body)
				require.NoError(t, err)
				body = gz
			} else {
				assert.Empty(t, rec.Header().Get("Content-Encoding"))
			}
			var page fetcher.RemoteLogs
			require.NoError(t, json.NewDecoder(body).Decode(&page))
			assert.Equal(t, int64(7), page.Epoch)
			require.Len(t, page.Entries, 1)
			assert.Equal(t, "hello", page.Entries[0].Message)
		})
	}
}
