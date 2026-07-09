package handlers

import (
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// RequestLogger debug-logs each request with method, URI, proto and duration.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		next.ServeHTTP(w, r)
		log.Debug().
			Str("method", r.Method).
			Str("request_uri", r.RequestURI).
			Str("proto", r.Proto).
			Float64("duration_seconds", time.Since(t).Seconds()).
			Msg("Incoming request")
	})
}

// Recovery converts a handler panic into a 500 instead of killing the
// connection/process (replaces gin.Recovery; the collector additionally has
// its own scrape-path recover that degrades to tdarr_up=0).
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error().Interface("panic", rec).Str("request_uri", r.RequestURI).Msg("Panic in HTTP handler")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
