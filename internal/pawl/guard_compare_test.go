package pawl_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	pawl "github.com/tiangong-dev/pawl/internal/pawl"
)

// The scalar comparison keeps its own entry point: GuardViolations(base, pr)
// never reads a breakdown, so a gated rise behind a flat total is invisible
// to it, and GuardViolationsWithGates with no gate map is that same comparison.
func TestGuardViolationsStaysScalarOnly(t *testing.T) {
	base := map[string]pawl.Metric{"a": {
		Direction: pawl.LowerIsBetter, Value: 3,
		Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1, "a.go:3": 1},
	}}
	pr := map[string]pawl.Metric{"a": {
		Direction: pawl.LowerIsBetter, Value: 3,
		Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1, "b.go:7": 1},
	}}

	t.Run("a flat total with a per-file rise is not a violation", func(t *testing.T) {
		violations, removed := pawl.GuardViolations(base, pr)
		wantViolations(t, violations, nil)
		wantLines(t, removed, nil)
	})

	t.Run("no gate map is that same comparison", func(t *testing.T) {
		withNilGates, removed := pawl.GuardViolationsWithGates(base, pr, nil)
		scalarOnly, _ := pawl.GuardViolations(base, pr)
		if !reflect.DeepEqual(withNilGates, scalarOnly) {
			t.Fatalf("nil gates = %#v, GuardViolations = %#v", withNilGates, scalarOnly)
		}
		wantViolations(t, withNilGates, nil)
		wantLines(t, removed, nil)
	})

	t.Run("a scalar regression is still reported", func(t *testing.T) {
		worse := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 4}}
		violations, _ := pawl.GuardViolations(base, worse)
		wantViolations(t, violations, []string{"a: 3 → 4"})
	})
}

// Configured gate predicates are compared the way check compares them, while
// other gates retain their scalar-only comparison.
func TestGuardViolationsPerFileCount(t *testing.T) {
	gates := map[string]pawl.GateMode{"a": pawl.GatePerFileCount}

	t.Run("a file count that rose behind a flat total is a violation", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 3,
			Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1, "a.go:3": 1},
		}}
		pr := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 3,
			Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1, "b.go:7": 1},
		}}
		violations, _ := pawl.GuardViolationsWithGates(base, pr, gates)
		wantViolations(t, violations, []string{"a: 3 → 3 (b.go 0 → 1)"})
	})

	t.Run("a file count that rose behind an improved total is still a violation", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 3,
			Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1, "a.go:3": 1},
		}}
		pr := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 2,
			Breakdown: map[string]float64{"a.go:9": 1, "b.go:7": 1},
		}}
		violations, _ := pawl.GuardViolationsWithGates(base, pr, gates)
		wantViolations(t, violations, []string{"a: 3 → 2 (b.go 0 → 1)"})
	})

	t.Run("every file equal or lower is not a violation", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 3,
			Breakdown: map[string]float64{"a.go:1": 2, "b.go:4": 1},
		}}
		pr := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 2,
			Breakdown: map[string]float64{"a.go:1": 2},
		}}
		violations, _ := pawl.GuardViolationsWithGates(base, pr, gates)
		wantViolations(t, violations, nil)
	})

	t.Run("a file only in the newer snapshot regresses from zero", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 0, Breakdown: nil}}
		pr := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 1,
			Breakdown: map[string]float64{"new.go:1": 1},
		}}
		violations, _ := pawl.GuardViolationsWithGates(base, pr, gates)
		wantViolations(t, violations, []string{"a: 0 → 1 (new.go 0 → 1)"})
	})

	t.Run("a total regression also names the files behind it", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 1,
			Breakdown: map[string]float64{"a.go:1": 1},
		}}
		pr := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Value: 3,
			Breakdown: map[string]float64{"a.go:1": 2, "b.go:1": 1},
		}}
		violations, _ := pawl.GuardViolationsWithGates(base, pr, gates)
		wantViolations(t, violations, []string{"a: 1 → 3 (a.go 1 → 2; b.go 0 → 1)"})
	})

	t.Run("more than ten files are summarized", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 0}}
		breakdown := map[string]float64{}
		for _, file := range []string{"f01.go", "f02.go", "f03.go", "f04.go", "f05.go", "f06.go", "f07.go", "f08.go", "f09.go", "f10.go", "f11.go", "f12.go"} {
			breakdown[file+":1"] = 1
		}
		pr := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 12, Breakdown: breakdown}}
		violations, _ := pawl.GuardViolationsWithGates(base, pr, gates)
		wantViolations(t, violations, []string{
			"a: 0 → 12 (f01.go 0 → 1; f02.go 0 → 1; f03.go 0 → 1; f04.go 0 → 1; f05.go 0 → 1; " +
				"f06.go 0 → 1; f07.go 0 → 1; f08.go 0 → 1; f09.go 0 → 1; f10.go 0 → 1; and 2 more)",
		})
	})

	t.Run("per-key-value and total gates ignore per-file movement", func(t *testing.T) {
		base := map[string]pawl.Metric{
			"k": {Direction: pawl.LowerIsBetter, Value: 2, Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1}},
			"t": {Direction: pawl.LowerIsBetter, Value: 1, Breakdown: map[string]float64{"a.go:1": 1}},
		}
		pr := map[string]pawl.Metric{
			"k": {Direction: pawl.LowerIsBetter, Value: 2, Breakdown: map[string]float64{"a.go:2": 1, "b.go:9": 1}},
			"t": {Direction: pawl.LowerIsBetter, Value: 1, Breakdown: map[string]float64{"b.go:9": 1}},
		}
		violations, _ := pawl.GuardViolationsWithGates(base, pr, map[string]pawl.GateMode{
			"k": pawl.GatePerKeyValue,
			"t": pawl.GateTotal,
		})
		wantViolations(t, violations, nil)
	})

	t.Run("a shared per-key-value that worsens is reported despite a flat scalar", func(t *testing.T) {
		base := map[string]pawl.Metric{"p": {
			Direction: pawl.LowerIsBetter, Value: 2,
			Breakdown: map[string]float64{"k1": 1, "k2": 1},
		}}
		pr := map[string]pawl.Metric{"p": {
			Direction: pawl.LowerIsBetter, Value: 2,
			Breakdown: map[string]float64{"k1": 3, "k2": 0},
		}}
		violations, _ := pawl.GuardViolationsWithGates(base, pr, map[string]pawl.GateMode{"p": pawl.GatePerKeyValue})
		wantViolations(t, violations, []string{"p: 2 → 2 (k1 1 → 3)"})
	})

	t.Run("per-key-value uses direction and recorded tolerance", func(t *testing.T) {
		for _, tc := range []struct {
			direction     pawl.Direction
			base, current float64
			want          bool
		}{
			{pawl.LowerIsBetter, 10, 12, false}, {pawl.LowerIsBetter, 10, 13, true},
			{pawl.HigherIsBetter, 10, 8, false}, {pawl.HigherIsBetter, 10, 7, true},
		} {
			tolerance := 2.0
			base := map[string]pawl.Metric{"a": {Direction: tc.direction, Value: 1, Tolerance: &tolerance, Breakdown: map[string]float64{"same": tc.base, "old": 4}}}
			pr := map[string]pawl.Metric{"a": {Direction: tc.direction, Value: 1, Tolerance: &tolerance, Breakdown: map[string]float64{"same": tc.current, "new": 99}}}
			got, _ := pawl.GuardViolationsWithGates(base, pr, map[string]pawl.GateMode{"a": pawl.GatePerKeyValue})
			if (len(got) > 0) != tc.want {
				t.Fatalf("direction %s, %v → %v: violations %#v, want=%v", tc.direction, tc.base, tc.current, got, tc.want)
			}
		}
	})

	t.Run("per-key detail is capped", func(t *testing.T) {
		baseBreakdown, currentBreakdown := map[string]float64{}, map[string]float64{}
		for i := 0; i < 12; i++ {
			key := fmt.Sprintf("f%02d.go:1", i)
			baseBreakdown[key], currentBreakdown[key] = 1, 3
		}
		base := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 12, Breakdown: baseBreakdown}}
		current := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 12, Breakdown: currentBreakdown}}
		violations, _ := pawl.GuardViolationsWithGates(base, current, map[string]pawl.GateMode{"a": pawl.GatePerKeyValue})
		line := violations[0].String()
		if !strings.Contains(line, "and 2 more") || strings.Count(line, " → ") != 11 {
			t.Fatalf("key detail should list ten entries and summary: %s", line)
		}
	})

	t.Run("a redefined metric stays out of the comparison", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Definition: "v1:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Value: 1, Breakdown: map[string]float64{"a.go:1": 1},
		}}
		pr := map[string]pawl.Metric{"a": {
			Direction: pawl.LowerIsBetter, Definition: "v1:sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Value: 5, Breakdown: map[string]float64{"a.go:1": 5},
		}}
		violations, _ := pawl.GuardViolationsWithGates(base, pr, gates)
		wantViolations(t, violations, nil)
	})
}

// The per-file detail is capped so a wide regression stays one line, and a
// violation with no per-file detail keeps the scalar-only rendering.
func TestFormatFileGrowth(t *testing.T) {
	gates := map[string]pawl.GateMode{"a": pawl.GatePerFileCount}

	t.Run("a scalar-only violation renders without a file list", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 3}}
		pr := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 4}}
		violations, _ := pawl.GuardViolationsWithGates(base, pr, gates)
		wantViolations(t, violations, []string{"a: 3 → 4"})
	})

	t.Run("exactly the cap lists every file with no summary", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 0}}
		breakdown := map[string]float64{}
		for _, file := range []string{"f01.go", "f02.go", "f03.go", "f04.go", "f05.go", "f06.go", "f07.go", "f08.go", "f09.go", "f10.go"} {
			breakdown[file+":1"] = 1
		}
		pr := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 10, Breakdown: breakdown}}
		violations, _ := pawl.GuardViolationsWithGates(base, pr, gates)
		got := violations[0].String()
		if strings.Contains(got, "more") {
			t.Fatalf("ten files must not be summarized: %s", got)
		}
		if strings.Count(got, ".go 0 → 1") != 10 {
			t.Fatalf("want ten file entries, got %s", got)
		}
	})
}
