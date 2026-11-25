package agent

import "testing"

// TestTruncatePassesThroughWithinLimit proves input at or below the byte limit
// is returned unchanged.
func TestTruncatePassesThroughWithinLimit(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{name: "empty", in: "", limit: 4, want: ""},
		{name: "under limit", in: "ex", limit: 4, want: "ex"},
		{name: "at limit", in: "ex-a", limit: 4, want: "ex-a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.in, tt.limit); got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.limit, got, tt.want)
			}
		})
	}
}

// TestTruncateAppendsEllipsisOverLimit proves input over the byte limit is cut
// to the limit and gets a trailing ellipsis.
func TestTruncateAppendsEllipsisOverLimit(t *testing.T) {
	const (
		in    = "ex-answer"
		limit = 4
		want  = "ex-a..."
	)
	if got := truncate(in, limit); got != want {
		t.Errorf("truncate(%q, %d) = %q, want %q", in, limit, got, want)
	}
}

// TestTruncateDoesNotSplitUTF8 proves the cut backs up to a rune boundary.
func TestTruncateDoesNotSplitUTF8(t *testing.T) {
	const (
		in    = "ex-é"
		limit = 4
		want  = "ex-..."
	)
	if got := truncate(in, limit); got != want {
		t.Errorf("truncate(%q, %d) = %q, want %q", in, limit, got, want)
	}
}
