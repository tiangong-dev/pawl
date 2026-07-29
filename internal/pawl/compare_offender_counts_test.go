package pawl_test

import (
	"reflect"
	"testing"

	pawl "github.com/tiangong-dev/pawl/internal/pawl"
)

// OffenderCountsByFile sums finding multiplicity grouped by path. Multiple
// findings on one line must remain multiple offenders.
func TestOffenderCountsByFile(t *testing.T) {
	cases := []struct {
		name      string
		breakdown map[string]float64
		want      map[string]float64
	}{
		{
			name:      "nil breakdown yields empty result",
			breakdown: nil,
			want:      map[string]float64{},
		},
		{
			name:      "empty breakdown yields empty result",
			breakdown: map[string]float64{},
			want:      map[string]float64{},
		},
		{
			name: "groups path:line keys by file part",
			breakdown: map[string]float64{
				"a.go:1": 1,
				"a.go:2": 1,
				"b.go:5": 1,
			},
			want: map[string]float64{"a.go": 2, "b.go": 1},
		},
		{
			name: "key without colon counts as its own file",
			breakdown: map[string]float64{
				"standalone-file.go": 1,
			},
			want: map[string]float64{"standalone-file.go": 1},
		},
		{
			name: "a non-location suffix leaves the key file-only",
			breakdown: map[string]float64{
				"a.go:1:extra": 1,
				"a.go:2":       1,
			},
			want: map[string]float64{"a.go": 1, "a.go:1:extra": 1},
		},
		{
			name: "sums values at each location",
			breakdown: map[string]float64{
				"a.go:1": 5,
				"a.go:2": 100,
			},
			want: map[string]float64{"a.go": 105},
		},
		{
			name: "mix of colon and non-colon keys",
			breakdown: map[string]float64{
				"a.go:1":    1,
				"a.go:2":    1,
				"whole-dim": 1,
			},
			want: map[string]float64{"a.go": 2, "whole-dim": 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pawl.OffenderCountsByFile(tc.breakdown)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("OffenderCountsByFile(%v) = %v, want %v", tc.breakdown, got, tc.want)
			}
		})
	}
}
