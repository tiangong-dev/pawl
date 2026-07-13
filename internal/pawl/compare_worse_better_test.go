package pawl_test

import (
	"testing"

	pawl "github.com/tiangong-dev/pawl/internal/pawl"
)

// Worse implements: tolerance is absolute slack in the worse direction, and a
// value exactly AT the tolerance boundary passes (is not worse).
func TestWorse(t *testing.T) {
	cases := []struct {
		name      string
		dir       pawl.Direction
		base, cur float64
		tolerance float64
		want      bool
	}{
		{"higher-is-better: drop is worse", pawl.HigherIsBetter, 10, 9, 0, true},
		{"higher-is-better: unchanged is not worse", pawl.HigherIsBetter, 10, 10, 0, false},
		{"higher-is-better: rise is not worse", pawl.HigherIsBetter, 10, 11, 0, false},
		{"higher-is-better: exactly at tolerance boundary passes", pawl.HigherIsBetter, 10, 8, 2, false},
		{"higher-is-better: just past tolerance boundary is worse", pawl.HigherIsBetter, 10, 7.99, 2, true},
		{"higher-is-better: just inside tolerance boundary is not worse", pawl.HigherIsBetter, 10, 8.01, 2, false},

		{"lower-is-better: rise is worse", pawl.LowerIsBetter, 10, 11, 0, true},
		{"lower-is-better: unchanged is not worse", pawl.LowerIsBetter, 10, 10, 0, false},
		{"lower-is-better: drop is not worse", pawl.LowerIsBetter, 10, 9, 0, false},
		{"lower-is-better: exactly at tolerance boundary passes", pawl.LowerIsBetter, 10, 11, 1, false},
		{"lower-is-better: just past tolerance boundary is worse", pawl.LowerIsBetter, 10, 11.01, 1, true},
		{"lower-is-better: just inside tolerance boundary is not worse", pawl.LowerIsBetter, 10, 10.99, 1, false},

		{"lower-is-better: zero tolerance boundary passes at equality", pawl.LowerIsBetter, 5, 5, 0, false},
		{"higher-is-better: zero tolerance boundary passes at equality", pawl.HigherIsBetter, 5, 5, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pawl.Worse(tc.dir, tc.base, tc.cur, tc.tolerance)
			if got != tc.want {
				t.Errorf("Worse(%v, base=%v, cur=%v, tol=%v) = %v, want %v",
					tc.dir, tc.base, tc.cur, tc.tolerance, got, tc.want)
			}
		})
	}
}

// Better is strict improvement and never reads a tolerance argument — equality
// is never "better", in either direction.
func TestBetter(t *testing.T) {
	cases := []struct {
		name      string
		dir       pawl.Direction
		base, cur float64
		want      bool
	}{
		{"higher-is-better: rise is better", pawl.HigherIsBetter, 10, 11, true},
		{"higher-is-better: unchanged is not better", pawl.HigherIsBetter, 10, 10, false},
		{"higher-is-better: drop is not better", pawl.HigherIsBetter, 10, 9, false},
		{"higher-is-better: tiny rise is still strictly better", pawl.HigherIsBetter, 10, 10.0001, true},

		{"lower-is-better: drop is better", pawl.LowerIsBetter, 10, 9, true},
		{"lower-is-better: unchanged is not better", pawl.LowerIsBetter, 10, 10, false},
		{"lower-is-better: rise is not better", pawl.LowerIsBetter, 10, 11, false},
		{"lower-is-better: tiny drop is still strictly better", pawl.LowerIsBetter, 10, 9.9999, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pawl.Better(tc.dir, tc.base, tc.cur)
			if got != tc.want {
				t.Errorf("Better(%v, base=%v, cur=%v) = %v, want %v", tc.dir, tc.base, tc.cur, got, tc.want)
			}
		})
	}
}
