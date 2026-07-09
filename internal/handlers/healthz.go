package handlers

import (
	"encoding/json"
	"net/http"
)

// http server structs
type InternalHealth struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// HealthzHandler reports exporter process liveness (not Tdarr reachability —
// that is tdarr_up's job).
func HealthzHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(InternalHealth{Status: "ok"})
	})
}
