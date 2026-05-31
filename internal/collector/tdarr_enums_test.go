package collector

import (
	"testing"
)

// ---------------------------------------------------------------------------
// cleanTranscodeLabel
// ---------------------------------------------------------------------------

func TestCleanTranscodeLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "Not required lowercased",
			input: "Not required",
			want:  "not required",
		},
		{
			name:  "Transcode error strips prefix",
			input: "Transcode error",
			want:  "error",
		},
		{
			name:  "Queued lowercased",
			input: "Queued",
			want:  "queued",
		},
		{
			name:  "Success lowercased",
			input: "Success",
			want:  "success",
		},
		{
			name:  "already lowercase transcode prefix stripped",
			input: "transcodecpu",
			want:  "cpu",
		},
		{
			name:  "empty string returns empty",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace-only after trim",
			input: "transcode ",
			// "transcode " -> lower -> "transcode " -> TrimPrefix("transcode") -> " " -> TrimSpace -> ""
			want: "",
		},
		{
			name:  "no transcode prefix, just lowercase",
			input: "Cancelled",
			want:  "cancelled",
		},
		{
			name:  "Hold lowercased",
			input: "Hold",
			want:  "hold",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cleanTranscodeLabel(tc.input)
			if got != tc.want {
				t.Errorf("cleanTranscodeLabel(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// cleanHealthCheckLabel
// ---------------------------------------------------------------------------

func TestCleanHealthCheckLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "Queued lowercased",
			input: "Queued",
			want:  "queued",
		},
		{
			name:  "Error lowercased",
			input: "Error",
			want:  "error",
		},
		{
			name:  "Success lowercased",
			input: "Success",
			want:  "success",
		},
		{
			name:  "Cancelled lowercased",
			input: "Cancelled",
			want:  "cancelled",
		},
		{
			name:  "already lowercase unchanged",
			input: "queued",
			want:  "queued",
		},
		{
			name:  "mixed case",
			input: "QUEUED",
			want:  "queued",
		},
		{
			name:  "empty string returns empty",
			input: "",
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cleanHealthCheckLabel(tc.input)
			if got != tc.want {
				t.Errorf("cleanHealthCheckLabel(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizeStatusSlice
// ---------------------------------------------------------------------------

func TestNormalizeStatusSlice(t *testing.T) {
	t.Parallel()

	// knownForTest is a small known set used by most sub-tests to avoid coupling to
	// the production knownTranscodeStatuses / knownHealthCheckStatuses sets.
	knownForTest := map[string]struct{}{
		"queued":  {},
		"error":   {},
		"success": {},
	}
	identityCleaner := func(s string) string { return s }

	t.Run("empty raw slice populates all known statuses as zero", func(t *testing.T) {
		t.Parallel()
		result := normalizeStatusSlice(nil, knownForTest, identityCleaner, "transcode", "lib1", nil)

		if len(result) != len(knownForTest) {
			t.Fatalf("want %d entries, got %d: %v", len(knownForTest), len(result), result)
		}
		for k := range knownForTest {
			val, ok := result[k]
			if !ok {
				t.Errorf("key %q missing from result", k)
				continue
			}
			if val != 0 {
				t.Errorf("result[%q] = %d, want 0", k, val)
			}
		}
	})

	t.Run("known statuses are stored with their API values", func(t *testing.T) {
		t.Parallel()
		raw := []TdarrPieSlice{
			{Name: "queued", Value: 10},
			{Name: "success", Value: 5},
			// "error" absent → stays 0
		}
		result := normalizeStatusSlice(raw, knownForTest, identityCleaner, "transcode", "lib1", nil)

		if result["queued"] != 10 {
			t.Errorf("result[queued] = %d, want 10", result["queued"])
		}
		if result["success"] != 5 {
			t.Errorf("result[success] = %d, want 5", result["success"])
		}
		if result["error"] != 0 {
			t.Errorf("result[error] = %d, want 0 (absent in raw)", result["error"])
		}
	})

	t.Run("cleaner function is applied before known-set lookup", func(t *testing.T) {
		t.Parallel()
		raw := []TdarrPieSlice{
			{Name: "Transcode error", Value: 7},
		}
		// Use cleanTranscodeLabel: "Transcode error" -> "error".
		knownWithError := map[string]struct{}{
			"queued": {},
			"error":  {},
		}
		result := normalizeStatusSlice(raw, knownWithError, cleanTranscodeLabel, "transcode", "lib1", nil)
		if result["error"] != 7 {
			t.Errorf("result[error] = %d, want 7 after label cleaning", result["error"])
		}
	})

	t.Run("unknown status: stored with real value and unknownCounter called", func(t *testing.T) {
		t.Parallel()
		raw := []TdarrPieSlice{
			{Name: "futurestatus", Value: 3},
		}
		var counterCalls []struct{ kind, status string }
		counter := func(kind, status string) {
			counterCalls = append(counterCalls, struct{ kind, status string }{kind, status})
		}
		result := normalizeStatusSlice(raw, knownForTest, identityCleaner, "transcode", "lib99", counter)

		// Observable effect 1: the unknown status is present in result with its real value.
		if result["futurestatus"] != 3 {
			t.Errorf("result[futurestatus] = %d, want 3 (unknown status should not be discarded)", result["futurestatus"])
		}
		// Observable effect 2: unknownCounter was invoked exactly once with the right args.
		if len(counterCalls) != 1 {
			t.Fatalf("unknownCounter calls: want 1, got %d", len(counterCalls))
		}
		if counterCalls[0].kind != "transcode" {
			t.Errorf("unknownCounter kind = %q, want %q", counterCalls[0].kind, "transcode")
		}
		if counterCalls[0].status != "futurestatus" {
			t.Errorf("unknownCounter status = %q, want %q", counterCalls[0].status, "futurestatus")
		}
	})

	t.Run("unknown status: known statuses are still zero-padded", func(t *testing.T) {
		t.Parallel()
		raw := []TdarrPieSlice{
			{Name: "alientype", Value: 99},
		}
		result := normalizeStatusSlice(raw, knownForTest, identityCleaner, "transcode", "lib1", nil)

		for k := range knownForTest {
			if result[k] != 0 {
				t.Errorf("result[%q] = %d, want 0 (known status absent from raw should be 0)", k, result[k])
			}
		}
	})

	t.Run("unknown status: nil unknownCounter does not panic", func(t *testing.T) {
		t.Parallel()
		raw := []TdarrPieSlice{
			{Name: "mysterystatus", Value: 1},
		}
		// Must not panic even when unknownCounter is nil.
		result := normalizeStatusSlice(raw, knownForTest, identityCleaner, "transcode", "lib1", nil)
		if result["mysterystatus"] != 1 {
			t.Errorf("result[mysterystatus] = %d, want 1", result["mysterystatus"])
		}
	})

	t.Run("multiple unknown statuses each invoke counter and are distinct in result", func(t *testing.T) {
		t.Parallel()
		raw := []TdarrPieSlice{
			{Name: "alpha", Value: 2},
			{Name: "beta", Value: 5},
		}
		callCount := 0
		counter := func(kind, status string) { callCount++ }
		result := normalizeStatusSlice(raw, knownForTest, identityCleaner, "healthcheck", "lib2", counter)

		if callCount != 2 {
			t.Errorf("unknownCounter calls: want 2, got %d", callCount)
		}
		if result["alpha"] != 2 {
			t.Errorf("result[alpha] = %d, want 2", result["alpha"])
		}
		if result["beta"] != 5 {
			t.Errorf("result[beta] = %d, want 5", result["beta"])
		}
	})

	t.Run("production transcode known set with real cleaner normalizes correctly", func(t *testing.T) {
		t.Parallel()
		raw := []TdarrPieSlice{
			{Name: "Not required", Value: 100},
			{Name: "Transcode error", Value: 4},
			{Name: "Queued", Value: 8},
			{Name: "Success", Value: 20},
			{Name: "Ignored", Value: 0},
			{Name: "Cancelled", Value: 1},
			{Name: "Hold", Value: 2},
		}
		var unknownCalls int
		counter := func(kind, status string) { unknownCalls++ }
		result := normalizeStatusSlice(raw, knownTranscodeStatuses, cleanTranscodeLabel, "transcode", "lib1", counter)

		// All entries are known; counter must not be called.
		if unknownCalls != 0 {
			t.Errorf("unknownCounter calls: want 0 (all known), got %d", unknownCalls)
		}
		want := map[string]int{
			"not required": 100,
			"error":        4,
			"queued":       8,
			"success":      20,
			"ignored":      0,
			"cancelled":    1,
			"hold":         2,
		}
		for k, wantVal := range want {
			if result[k] != wantVal {
				t.Errorf("result[%q] = %d, want %d", k, result[k], wantVal)
			}
		}
	})

	t.Run("production healthcheck known set with real cleaner normalizes correctly", func(t *testing.T) {
		t.Parallel()
		raw := []TdarrPieSlice{
			{Name: "Queued", Value: 3},
			{Name: "Error", Value: 1},
			{Name: "Success", Value: 50},
			// "Cancelled" absent → 0
		}
		var unknownCalls int
		counter := func(kind, status string) { unknownCalls++ }
		result := normalizeStatusSlice(raw, knownHealthCheckStatuses, cleanHealthCheckLabel, "healthcheck", "lib2", counter)

		if unknownCalls != 0 {
			t.Errorf("unknownCounter calls: want 0 (all known), got %d", unknownCalls)
		}
		if result["queued"] != 3 {
			t.Errorf("result[queued] = %d, want 3", result["queued"])
		}
		if result["error"] != 1 {
			t.Errorf("result[error] = %d, want 1", result["error"])
		}
		if result["success"] != 50 {
			t.Errorf("result[success] = %d, want 50", result["success"])
		}
		if result["cancelled"] != 0 {
			t.Errorf("result[cancelled] = %d, want 0 (absent in raw)", result["cancelled"])
		}
	})
}
