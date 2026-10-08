package hud

import (
	"testing"
	"time"
)

func TestFormatPillTime(t *testing.T) {
	for _, tc := range []struct {
		name    string
		elapsed time.Duration
		budget  time.Duration
		want    PillTime
	}{
		{name: "none", elapsed: 9 * time.Minute, want: PillTime{Elapsed: "9m"}},
		{name: "none first minute", elapsed: 59 * time.Second},
		{name: "none hours", elapsed: 90 * time.Minute, want: PillTime{Elapsed: "1h 30m"}},
		{name: "none exact hour", elapsed: time.Hour, want: PillTime{Elapsed: "1h"}},
		{name: "under", elapsed: 9 * time.Minute, budget: 45 * time.Minute, want: PillTime{"9m", "/ 45m", ""}},
		{name: "over", elapsed: 57 * time.Minute, budget: 45 * time.Minute, want: PillTime{"57m", "/ 45m", "+12m"}},
		{name: "at budget", elapsed: 45 * time.Minute, budget: 45 * time.Minute, want: PillTime{"45m", "/ 45m", ""}},
		{name: "first minute", budget: 45 * time.Minute, want: PillTime{"0m", "/ 45m", ""}},
		{name: "hour budget", elapsed: 9 * time.Minute, budget: 90 * time.Minute, want: PillTime{"9m", "/ 1h 30m", ""}},
		{name: "over hours", elapsed: 150 * time.Minute, budget: 45 * time.Minute, want: PillTime{"2h 30m", "/ 45m", "+1h 45m"}},
		{name: "over seconds", elapsed: 45*time.Minute + time.Second, budget: 45 * time.Minute, want: PillTime{"45m", "/ 45m", "+1s"}},
		{name: "over fractional second", elapsed: 45*time.Minute + time.Millisecond, budget: 45 * time.Minute, want: PillTime{"45m", "/ 45m", "+<1s"}},
		{name: "seconds budget", elapsed: 2 * time.Minute, budget: 90 * time.Second, want: PillTime{"2m", "/ 1m30s", "+30s"}},
		{name: "fractional budget", budget: 500 * time.Millisecond, want: PillTime{"0m", "/ 500ms", ""}},
		{name: "future start", elapsed: -time.Minute, budget: 45 * time.Minute, want: PillTime{"0m", "/ 45m", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatPillTime(tc.elapsed, tc.budget); got != tc.want {
				t.Fatalf("pill time = %+v, want %+v", got, tc.want)
			}
		})
	}
}
