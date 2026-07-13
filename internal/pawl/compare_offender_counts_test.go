package pawl_test

import (
	"reflect"
	"testing"

	pawl "github.com/tiangong-dev/pawl/internal/pawl"
)

// OffenderCountsByFile counts breakdown KEYS grouped by the key's file part
// (substring before the first ':'), not the values — this is what keeps the
// per-file-count gate robust to code moving around inside a file.
func TestOffenderCountsByFile(t *testing.T) {
	cases := []struct {
		name      string
		breakdown map[string]float64
		want      map[string]int
	}{
		{
			name:      "nil breakdown yields empty result",
			breakdown: nil,
			want:      map[string]int{},
		},
		{
			name:      "empty breakdown yields empty result",
			breakdown: map[string]float64{},
			want:      map[string]int{},
		},
		{
			name: "groups path:line keys by file part",
			breakdown: map[string]float64{
				"a.go:1": 1,
				"a.go:2": 1,
				"b.go:5": 1,
			},
			want: map[string]int{"a.go": 2, "b.go": 1},
		},
		{
			name: "key without colon counts as its own file",
			breakdown: map[string]float64{
				"standalone-file.go": 1,
			},
			want: map[string]int{"standalone-file.go": 1},
		},
		{
			name: "file part is substring before the FIRST colon only",
			breakdown: map[string]float64{
				"a.go:1:extra": 1,
				"a.go:2":       1,
			},
			want: map[string]int{"a.go": 2},
		},
		{
			name: "counts keys, not sums of values",
			breakdown: map[string]float64{
				"a.go:1": 5,
				"a.go:2": 100,
			},
			want: map[string]int{"a.go": 2},
		},
		{
			name: "mix of colon and non-colon keys",
			breakdown: map[string]float64{
				"a.go:1":    1,
				"a.go:2":    1,
				"whole-dim": 1,
			},
			want: map[string]int{"a.go": 2, "whole-dim": 1},
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
