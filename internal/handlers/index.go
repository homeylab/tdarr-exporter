package handlers

import (
	"fmt"
	"html"
	"net/http"
)

// IndexHandler serves the landing page at '/', linking to the metrics endpoint
// at its configured path (metricsPath) rather than a hardcoded '/metrics', so
// the link stays correct when prometheus_path is customized. The path is
// html-escaped defensively before interpolation into the anchor.
func IndexHandler(metricsPath string) http.Handler {
	link := html.EscapeString(metricsPath)
	body := fmt.Sprintf(`<h1>tdarr-exporter</h1><p><a href='%s'>metrics</a></p>`, link)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, body)
	})
}
