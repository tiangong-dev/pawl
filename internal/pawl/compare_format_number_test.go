package pawl_test

import (
	"strings"
	"testing"

	pawl "github.com/tiangong-dev/pawl/internal/pawl"
)

// FormatNumber prints minimal decimal notation — integers with no trailing
// decimal point, decimals with no more digits than needed, and never
// switches to exponent form regardless of magnitude.
func TestFormatNumber(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{3613, "3613"},
		{72.41, "72.41"},
		{1000000, "1000000"},
		{0, "0"},
		{-5, "-5"},
		{0.1, "0.1"},
		{1, "1"},
		{-72.41, "-72.41"},
		{10000000, "10000000"},
		{500, "500"},
		{501, "501"},
		{1.5, "1.5"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			got := pawl.FormatNumber(tc.in)
			if got != tc.want {
				t.Errorf("FormatNumber(%v) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.ContainsAny(got, "eE") {
				t.Errorf("FormatNumber(%v) = %q uses exponent form", tc.in, got)
			}
		})
	}
}

// Large magnitudes never fall back to exponent notation, unlike Go's default
// float formatting (%v / %g), which switches to scientific notation early.
func TestFormatNumberNeverUsesExponentForLargeValues(t *testing.T) {
	for _, v := range []float64{1e6, 1e9, 1e15, 1e20} {
		got := pawl.FormatNumber(v)
		if strings.ContainsAny(got, "eE") {
			t.Errorf("FormatNumber(%v) = %q uses exponent form", v, got)
		}
	}
}
