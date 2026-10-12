package exporter

import (
	"context"
	"maps"
	"net/http"
	"slices"

	"github.com/alexandre-daubois/ember/internal/fetcher"
)

func CertificatesHandler(holder *StateHolder, dial func(context.Context, []string) []fetcher.CertificateInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, dial(r.Context(), snapshotHosts(holder)))
	}
}

func snapshotHosts(holder *StateHolder) []string {
	slot, _ := holder.lookup("")
	if slot == nil || slot.state.Current == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(slot.state.Current.Metrics.Hosts))
}
