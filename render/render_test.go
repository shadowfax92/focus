package render

import "testing"

func TestFormatPctInt(t *testing.T) {
	for _, tc := range []struct {
		current, previous int
		want              string
	}{{2, 4, "-50%"}, {6, 4, "+50%"}, {0, 0, "—"}, {1, 0, "new"}} {
		if got := FormatPctInt(tc.current, tc.previous); got != tc.want {
			t.Errorf("FormatPctInt(%d, %d) = %q, want %q", tc.current, tc.previous, got, tc.want)
		}
	}
}

func TestSparkline(t *testing.T) {
	if got := Sparkline([]int{0, 1, 2}); got != " ▅█" {
		t.Fatalf("Sparkline = %q", got)
	}
}
