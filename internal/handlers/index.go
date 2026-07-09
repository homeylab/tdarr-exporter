package handlers

import (
	"fmt"
	"net/http"
)

// IndexHandler serves the landing page at '/'.
func IndexHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<h1>tdarr-exporter</h1><p><a href='/metrics'>metrics</a></p>`)
	})
}
