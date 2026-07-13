package handlers

import (
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// responseRecorder wraps http.ResponseWriter to capture the status code and
// whether anything was written. Unwrap exposes the underlying writer to
// http.ResponseController (Go 1.20+) so Flush/Hijack/etc. still reach it; we
// deliberately do NOT hand-implement Flusher/Hijacker/ReaderFrom, because faking
// an interface the underlying writer may not support is a footgun and the
// handlers here need none of them.
type responseRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

// newResponseRecorder seeds status with 200: net/http sends an implicit 200 when
// a handler returns without ever calling WriteHeader, so that is the status the
// client actually sees and the one the access log should report.
func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, status: http.StatusOK}
}

// WriteHeader and Write record the status/wrote state only AFTER forwarding
// succeeds, so if the underlying writer panics (e.g. net/http rejecting an
// out-of-range status code) nothing was committed, wrote stays false, and
// Recovery still emits a clean 500 rather than a bare implicit 200.
func (r *responseRecorder) WriteHeader(code int) {
	r.ResponseWriter.WriteHeader(code)
	if !r.wrote {
		r.status = code
		r.wrote = true
	}
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	return n, err
}

func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// RequestLogger debug-logs each request with method, URI, proto, status and duration.
//
// The log call is deferred so a panicking handler still produces an access-log
// line (Recovery, which wraps RequestLogger from the outside, owns the actual
// recover()/500 conversion; this defer just rides the same unwind, including
// for http.ErrAbortHandler panics that re-panic through Recovery).
//
// CAVEAT: on panic, the logged status is the recorder's value at unwind
// time (200 unless the handler already called WriteHeader/Write before
// panicking) — NOT the 500 Recovery's own outer recorder ends up writing.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := newResponseRecorder(w)
		t := time.Now()
		defer func() {
			log.Debug().
				Str("method", r.Method).
				Str("request_uri", r.RequestURI).
				Str("proto", r.Proto).
				Int("status", rec.status).
				Float64("duration_seconds", time.Since(t).Seconds()).
				Msg("Incoming request")
		}()
		next.ServeHTTP(rec, r)
	})
}

// Recovery converts a handler panic into a 500 instead of killing the
// connection/process (replaces gin.Recovery; the collector additionally has
// its own scrape-path recover that degrades to tdarr_up=0).
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := newResponseRecorder(w)
		defer func() {
			if rec := recover(); rec != nil {
				//nolint:errorlint // recover() returns the exact value panicked with; net/http panics with this sentinel unwrapped, so == is the idiom (net/http itself compares this way).
				if rec == http.ErrAbortHandler {
					// stdlib's sanctioned connection-abort signal; net/http
					// handles it, so let it continue unwinding.
					panic(rec)
				}
				log.Error().Interface("panic", rec).Str("request_uri", r.RequestURI).Msg("Panic in HTTP handler")
				if !recorder.wrote {
					http.Error(recorder, "Internal Server Error", http.StatusInternalServerError)
				}
			}
		}()
		next.ServeHTTP(recorder, r)
	})
}
