package collector

import (
	"strings"

	"github.com/rs/zerolog/log"
)

// knownTranscodeStatuses is the finite set of cleaned transcode status label values.
// Raw API names (e.g. "Not required", "Transcode error") are cleaned by normalizePieStatuses
// before storage. The emit loop consumes these cleaned labels directly.
var knownTranscodeStatuses = map[string]struct{}{
	"not required": {},
	"queued":       {},
	"error":        {},
	"success":      {},
}

// knownHealthCheckStatuses is the finite set of cleaned health check status label values.
var knownHealthCheckStatuses = map[string]struct{}{
	"queued":  {},
	"error":   {},
	"success": {},
}

// cleanTranscodeLabel converts a raw API transcode status name into the cleaned label value
// used in Prometheus metrics. Mirrors the logic in cleanUpTranscodeStatus but operates at
// normalize-time rather than emit-time.
//
// Examples:
//
//	"Not required"    -> "not required"
//	"Transcode error" -> "error"
//	"Queued"          -> "queued"
func cleanTranscodeLabel(rawName string) string {
	lower := strings.ToLower(rawName)
	if strings.HasPrefix(lower, "transcode") {
		lower = strings.TrimPrefix(lower, "transcode")
	}
	return strings.TrimSpace(lower)
}

// cleanHealthCheckLabel converts a raw API health check status name into the cleaned label
// value used in Prometheus metrics.
func cleanHealthCheckLabel(rawName string) string {
	return strings.ToLower(rawName)
}

// normalizePieStatuses converts the raw API status slices on pie into pre-cleaned label maps
// covering the full known enum. Results are stored in pie.NormalizedTranscodes and
// pie.NormalizedHealthChecks.
//
// Behavior:
//   - Known statuses: present with real value (or 0 if absent from API response).
//   - Unknown statuses: kept with real value (no data loss), warn-logged, and bumped in the
//     unknownStatusTotal counter so operators can alert on API drift.
//   - Empty/nil input slices: all known statuses emitted as 0.
func normalizePieStatuses(pie *TdarrPieStats, unknownCounter func(kind, status string)) {
	pie.NormalizedTranscodes = normalizeStatusSlice(
		pie.PieStats.Status.Transcode,
		knownTranscodeStatuses,
		cleanTranscodeLabel,
		"transcode",
		pie.libraryId,
		unknownCounter,
	)
	pie.NormalizedHealthChecks = normalizeStatusSlice(
		pie.PieStats.Status.HealthCheck,
		knownHealthCheckStatuses,
		cleanHealthCheckLabel,
		"healthcheck",
		pie.libraryId,
		unknownCounter,
	)
}

// normalizeStatusSlice builds a complete cleaned-label → value map for a single status kind.
func normalizeStatusSlice(
	raw []TdarrPieSlice,
	known map[string]struct{},
	cleaner func(string) string,
	kind string,
	libraryId string,
	unknownCounter func(kind, status string),
) map[string]int {
	result := make(map[string]int, len(known))

	// Pre-populate all known statuses with 0.
	for k := range known {
		result[k] = 0
	}

	// Process each API entry.
	for _, s := range raw {
		cleaned := cleaner(s.Name)
		if _, isKnown := known[cleaned]; isKnown {
			result[cleaned] = s.Value
		} else {
			// Unknown status: emit with real value but warn and bump counter.
			log.Warn().
				Str("kind", kind).
				Str("rawStatus", s.Name).
				Str("cleanedStatus", cleaned).
				Str("libraryId", libraryId).
				Msg("Unknown pie status encountered; will emit metric but zero-pad not applied for future scrapes")
			result[cleaned] = s.Value
			if unknownCounter != nil {
				unknownCounter(kind, cleaned)
			}
		}
	}

	return result
}
