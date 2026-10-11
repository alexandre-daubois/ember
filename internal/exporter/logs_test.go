package exporter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogsHandler_RejectsABadCursor(t *testing.T) {
	h := LogsHandler(nil)
	for _, query := range []string{"", "after=abc", "after=-2"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/logs?"+query, nil))
		assert.Equal(t, http.StatusBadRequest, rec.Code, query)
	}
}
