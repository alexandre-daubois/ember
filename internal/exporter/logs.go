package exporter

import (
	"net/http"
	"strconv"

	"github.com/alexandre-daubois/ember/internal/fetcher"
)

type LogSource interface {
	Since(after int64, limit int) ([]fetcher.LogEntry, int64, error)
}

func LogsHandler(src LogSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		if err != nil || after < -1 {
			http.Error(w, "after must be a cursor returned by /logs", http.StatusBadRequest)
			return
		}
		entries, next, err := src.Since(after, fetcher.MaxRemoteLogs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, fetcher.RemoteLogs{Next: next, Entries: entries})
	}
}
