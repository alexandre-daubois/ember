package exporter

import (
	"encoding/json"
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
		trace := fetcher.RemoteTraceOf(r.Context())
		q := r.URL.Query()
		after, err := strconv.ParseInt(q.Get("after"), 10, 64)
		if err != nil || after < -1 {
			trace.Refused = http.StatusBadRequest
			http.Error(w, "after must be a cursor returned by /logs", http.StatusBadRequest)
			return
		}
		trace.SessionOpened = after == -1
		epoch := src.Epoch()
		// A cursor from another daemon process: resume from the oldest entry.
		if q.Has("epoch") && q.Get("epoch") != strconv.FormatInt(epoch, 10) {
			after = 0
		}
		entries, next, err := src.Since(after, fetcher.MaxRemoteLogs)
		if err != nil {
			trace.Refused = http.StatusConflict
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		page, err := json.Marshal(fetcher.RemoteLogs{Epoch: epoch, Next: next, Entries: entries})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body, done := fetcher.CompressIfAccepted(w, r)
		defer done()
		_, _ = body.Write(page)
	}
}
