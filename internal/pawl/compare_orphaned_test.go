package pawl_test

import (
	"reflect"
	"testing"

	pawl "github.com/tiangong-dev/pawl/internal/pawl"
)

// A snapshot metric whose id matches no configured dimension is an orphan —
// deleting a dimension must also drop its metric, or a regression could hide
// behind a vanished measurement.
func TestOrphanedMetrics(t *testing.T) {
	cases := []struct {
		name         string
		dimensionIDs []string
		baseline     map[string]pawl.Metric
		want         []string
	}{
		{
			name:         "no baseline metrics yields no orphans",
			dimensionIDs: []string{"a"},
			baseline:     map[string]pawl.Metric{},
			want:         nil,
		},
		{
			name:         "every baseline metric claimed yields no orphans",
			dimensionIDs: []string{"a", "b"},
			baseline:     map[string]pawl.Metric{"a": {}, "b": {}},
			want:         nil,
		},
		{
			name:         "an unclaimed metric is an orphan",
			dimensionIDs: []string{"a", "b"},
			baseline:     map[string]pawl.Metric{"a": {}, "b": {}, "c": {}},
			want:         []string{"c"},
		},
		{
			name:         "multiple orphans are returned sorted",
			dimensionIDs: []string{"a"},
			baseline:     map[string]pawl.Metric{"a": {}, "z": {}, "c": {}, "b": {}},
			want:         []string{"b", "c", "z"},
		},
		{
			name:         "no configured dimensions orphans every metric",
			dimensionIDs: nil,
			baseline:     map[string]pawl.Metric{"x": {}, "y": {}},
			want:         []string{"x", "y"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pawl.OrphanedMetrics(tc.dimensionIDs, tc.baseline)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("OrphanedMetrics(%v, %v) = %v, want %v", tc.dimensionIDs, tc.baseline, got, tc.want)
			}
		})
	}
}
