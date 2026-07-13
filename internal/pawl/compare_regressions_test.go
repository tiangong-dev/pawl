package pawl_test

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	pawl "github.com/tiangong-dev/pawl/internal/pawl"
)

// The scalar total is always checked, with tolerance, regardless of gate
// mode — the per-file/per-key checks are additive on top of it, never a
// replacement for it.
func TestRegressionsOfScalarTotal(t *testing.T) {
	cases := []struct {
		name string
		gate pawl.GateMode
		tol  float64
		base float64
		cur  float64
		want []string
	}{
		{"empty gate behaves as total: regression", "", 0, 10, 12, []string{"total 10 → 12"}},
		{"empty gate behaves as total: no regression", "", 0, 10, 9, nil},
		{"explicit total: regression", pawl.GateTotal, 0, 10, 12, []string{"total 10 → 12"}},
		{"explicit total: no regression", pawl.GateTotal, 0, 10, 9, nil},
		{"tolerance absorbs small regression", pawl.GateTotal, 1, 10, 11, nil},
		{"tolerance boundary exactly passes", pawl.GateTotal, 2.5, 20, 22.5, nil},
		{"regression past tolerance boundary", pawl.GateTotal, 1, 10, 12, []string{"total 10 → 12"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: tc.gate, Tolerance: tc.tol}
			got := pawl.RegressionsOf(spec, pawl.MetricSample{Value: tc.base}, pawl.MetricSample{Value: tc.cur})
			assertLines(t, got, tc.want)
		})
	}
}

// per-file-count: offender count per file may not rise. Counting keys (not
// summing values) means moving offenders around inside one file is not a
// regression; a file only appearing in current regresses from an implicit 0.
func TestRegressionsOfPerFileCount(t *testing.T) {
	t.Run("a file gaining an offender regresses", func(t *testing.T) {
		spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerFileCount}
		base := pawl.MetricSample{Value: 2, Breakdown: map[string]float64{"a.go:1": 1, "b.go:1": 1}}
		cur := pawl.MetricSample{Value: 2, Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1}}
		got := pawl.RegressionsOf(spec, base, cur)
		assertLines(t, got, []string{"a.go  1 → 2"})
	})

	t.Run("a file present only in current regresses from zero", func(t *testing.T) {
		spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerFileCount}
		base := pawl.MetricSample{Value: 0, Breakdown: map[string]float64{}}
		cur := pawl.MetricSample{Value: 1, Breakdown: map[string]float64{"c.go:1": 1}}
		got := pawl.RegressionsOf(spec, base, cur)
		mustContain(t, got, "c.go  0 → 1")
	})

	t.Run("moving offenders within a file does not regress", func(t *testing.T) {
		spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerFileCount}
		base := pawl.MetricSample{Value: 2, Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1}}
		cur := pawl.MetricSample{Value: 2, Breakdown: map[string]float64{"a.go:5": 1, "a.go:9": 1}}
		got := pawl.RegressionsOf(spec, base, cur)
		assertLines(t, got, nil)
	})

	t.Run("a file losing offenders is not reported", func(t *testing.T) {
		spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerFileCount}
		base := pawl.MetricSample{Value: 2, Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1}}
		cur := pawl.MetricSample{Value: 1, Breakdown: map[string]float64{"a.go:1": 1}}
		got := pawl.RegressionsOf(spec, base, cur)
		assertLines(t, got, nil)
	})

	t.Run("tolerance does not apply to per-file counts", func(t *testing.T) {
		spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerFileCount, Tolerance: 5}
		base := pawl.MetricSample{Value: 1, Breakdown: map[string]float64{"a.go:1": 1}}
		cur := pawl.MetricSample{Value: 2, Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1}}
		got := pawl.RegressionsOf(spec, base, cur)
		mustContain(t, got, "a.go  1 → 2")
	})

	// The scalar total is unchanged (2 → 2) while file A improves and file B
	// worsens: the net-zero total must not mask the localized regression.
	t.Run("net-zero total does not mask a per-file regression", func(t *testing.T) {
		spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerFileCount}
		base := pawl.MetricSample{Value: 2, Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1}}
		cur := pawl.MetricSample{Value: 2, Breakdown: map[string]float64{"a.go:1": 1, "b.go:1": 1}}
		got := pawl.RegressionsOf(spec, base, cur)
		assertLines(t, got, []string{"b.go  0 → 1"})
	})
}

// per-key-value: every baseline breakdown key must not worsen (with
// tolerance). Keys missing from current are ignored (legitimate removal);
// keys new in current are ignored (no baseline to compare against).
func TestRegressionsOfPerKeyValue(t *testing.T) {
	t.Run("a worsened key regresses, unchanged keys are silent", func(t *testing.T) {
		spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerKeyValue}
		base := pawl.MetricSample{Value: 8, Breakdown: map[string]float64{"k1": 5, "k2": 3}}
		cur := pawl.MetricSample{Value: 11, Breakdown: map[string]float64{"k1": 5, "k2": 6}}
		got := pawl.RegressionsOf(spec, base, cur)
		mustContain(t, got, "k2  3 → 6")
		mustNotContain(t, got, "k1")
	})

	t.Run("a key missing from current is ignored", func(t *testing.T) {
		spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerKeyValue}
		base := pawl.MetricSample{Value: 10, Breakdown: map[string]float64{"k3": 10}}
		cur := pawl.MetricSample{Value: 0, Breakdown: map[string]float64{}}
		got := pawl.RegressionsOf(spec, base, cur)
		mustNotContain(t, got, "k3")
	})

	t.Run("a key new in current is ignored", func(t *testing.T) {
		spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerKeyValue}
		base := pawl.MetricSample{Value: 0, Breakdown: map[string]float64{}}
		cur := pawl.MetricSample{Value: 100, Breakdown: map[string]float64{"k4": 100}}
		got := pawl.RegressionsOf(spec, base, cur)
		mustNotContain(t, got, "k4")
	})

	t.Run("tolerance applies per key, boundary passes", func(t *testing.T) {
		spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerKeyValue, Tolerance: 2}
		base := pawl.MetricSample{Value: 5, Breakdown: map[string]float64{"k1": 5}}
		cur := pawl.MetricSample{Value: 7, Breakdown: map[string]float64{"k1": 7}}
		got := pawl.RegressionsOf(spec, base, cur)
		assertLines(t, got, nil)
	})

	t.Run("tolerance applies per key, past boundary regresses", func(t *testing.T) {
		spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerKeyValue, Tolerance: 2}
		base := pawl.MetricSample{Value: 5, Breakdown: map[string]float64{"k1": 5}}
		cur := pawl.MetricSample{Value: 7.01, Breakdown: map[string]float64{"k1": 7.01}}
		got := pawl.RegressionsOf(spec, base, cur)
		mustContain(t, got, "k1  5 → 7.01")
	})
}

func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(got)
	sort.Strings(want)
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("regression lines = %#v, want %#v", got, want)
	}
}

func mustContain(t *testing.T, lines []string, want string) {
	t.Helper()
	for _, l := range lines {
		if l == want {
			return
		}
	}
	t.Fatalf("regression lines %#v do not contain %q", lines, want)
}

func mustNotContain(t *testing.T, lines []string, substr string) {
	t.Helper()
	for _, l := range lines {
		if strings.Contains(l, substr) {
			t.Fatalf("regression lines %#v unexpectedly mention %q", lines, substr)
		}
	}
}
