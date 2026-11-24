package pawl_test

import (
	"reflect"
	"testing"

	pawl "github.com/tiangong-dev/pawl/internal/pawl"
)

// A git-detected rename re-keys the base counts onto the new path before the
// per-file comparison, so moving a file is not itself new debt. An offender
// added in the same change still fails, and its line names the base path the
// count really came from.
func TestGuardViolationsMapRenamedFile(t *testing.T) {
	gates := map[string]pawl.GateMode{"a": pawl.GatePerFileCount}
	renamed := map[string]string{"a.go": "b.go"}
	base := map[string]pawl.Metric{"a": {
		Direction: pawl.LowerIsBetter, Value: 3,
		Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1, "a.go:3": 1},
	}}

	t.Run("the same offenders under a new name are not a regression", func(t *testing.T) {
		pr := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 3,
			Breakdown: map[string]float64{"b.go:1": 1, "b.go:2": 1, "b.go:3": 1},
		}}
		withoutRenames, _ := pawl.GuardViolationsWithGates(base, pr, gates)
		wantViolations(t, withoutRenames, []string{"a: 3 → 3 (b.go 0 → 3)"})

		violations, _ := pawl.GuardViolationsWithGatesAndRenames(base, pr, gates, renamed)
		wantViolations(t, violations, nil)
	})

	t.Run("an offender added in the renamed file still fails", func(t *testing.T) {
		pr := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 4,
			Breakdown: map[string]float64{"b.go:1": 1, "b.go:2": 1, "b.go:3": 1, "b.go:4": 1},
		}}
		violations, _ := pawl.GuardViolationsWithGatesAndRenames(base, pr, gates, renamed)
		wantViolations(t, violations, []string{"a: 3 → 4 (b.go 3 → 4 (renamed from a.go))"})
	})

	t.Run("a gate other than per-file-count compares the same with and without renames", func(t *testing.T) {
		pr := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 3,
			Breakdown: map[string]float64{"b.go:1": 1, "b.go:2": 1, "b.go:3": 1},
		}}
		for _, gate := range []pawl.GateMode{pawl.GateTotal, pawl.GatePerKeyValue} {
			gateMap := map[string]pawl.GateMode{"a": gate}
			withRenames, _ := pawl.GuardViolationsWithGatesAndRenames(base, pr, gateMap, renamed)
			without, _ := pawl.GuardViolationsWithGates(base, pr, gateMap)
			if !reflect.DeepEqual(withRenames, without) {
				t.Fatalf("gate %s: renames changed the comparison: %#v vs %#v", gate, withRenames, without)
			}
		}
	})
}

// A rename is only applied when the map is unambiguous and the base has no
// count for the destination: everything else falls back to the plain per-path
// comparison, which is the stricter reading.
func TestGuardRenameMappingStaysConservative(t *testing.T) {
	gates := map[string]pawl.GateMode{"a": pawl.GatePerFileCount}

	t.Run("a destination the base already records is not mapped", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 4,
			Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1, "a.go:3": 1, "b.go:9": 1},
		}}
		pr := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 4,
			Breakdown: map[string]float64{"b.go:1": 1, "b.go:2": 1, "b.go:3": 1, "b.go:9": 1},
		}}
		violations, _ := pawl.GuardViolationsWithGatesAndRenames(base, pr, gates, map[string]string{"a.go": "b.go"})
		wantViolations(t, violations, []string{"a: 4 → 4 (b.go 1 → 4)"})
	})

	t.Run("two sources claiming one destination are not mapped", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 5,
			Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1, "a.go:3": 1, "b.go:1": 1, "b.go:2": 1},
		}}
		pr := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 4,
			Breakdown: map[string]float64{"c.go:1": 1, "c.go:2": 1, "c.go:3": 1, "c.go:4": 1},
		}}
		violations, _ := pawl.GuardViolationsWithGatesAndRenames(base, pr, gates,
			map[string]string{"a.go": "c.go", "b.go": "c.go"})
		wantViolations(t, violations, []string{"a: 5 → 4 (c.go 0 → 4)"})
	})

	t.Run("a rename whose source the base does not record changes nothing", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 3,
			Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1, "a.go:3": 1},
		}}
		pr := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 3,
			Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1, "a.go:3": 1},
		}}
		violations, _ := pawl.GuardViolationsWithGatesAndRenames(base, pr, gates, map[string]string{"gone.go": "new.go"})
		wantViolations(t, violations, nil)
	})

	t.Run("a breakdown-less metric is compared without panicking", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 0}}
		pr := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 1,
			Breakdown: map[string]float64{"b.go:1": 1}}}
		violations, _ := pawl.GuardViolationsWithGatesAndRenames(base, pr, gates, map[string]string{"a.go": "b.go"})
		wantViolations(t, violations, []string{"a: 0 → 1 (b.go 0 → 1)"})
	})
}
