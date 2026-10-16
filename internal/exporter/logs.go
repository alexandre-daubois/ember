package exporter

import (
	"net/http"
	"strconv"

	"github.com/alexandre-daubois/ember/internal/fetcher"
)

type LogSource interface {
	Epoch() int64
	Since(after int64, limit int) ([]fetcher.LogEntry, int64, error)
}

func LogsHandler(src LogSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		after, err := strconv.ParseInt(q.Get("after"), 10, 64)
		if err != nil || after < -1 {
			http.Error(w, "after must be a cursor returned by /logs", http.StatusBadRequest)
			return
		}
		epoch := src.Epoch()
		// A cursor from another daemon process: resume from the oldest entry.
		if q.Has("epoch") && q.Get("epoch") != strconv.FormatInt(epoch, 10) {
			after = 0
		}
		entries, next, err := src.Since(after, fetcher.MaxRemoteLogs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, fetcher.RemoteLogs{Epoch: epoch, Next: next, Entries: entries})
	}
}
