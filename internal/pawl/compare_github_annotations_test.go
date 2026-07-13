package pawl_test

import (
	"reflect"
	"testing"

	pawl "github.com/tiangong-dev/pawl/internal/pawl"
)

// per-file-count: only keys that are new in cur (absent from base) AND sit in
// a file whose offender count rose are annotated. An unchanged key inside a
// file that also gained a new key must not itself produce a line.
func TestGitHubAnnotationsPerFileCountNewKeysOnly(t *testing.T) {
	spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerFileCount}
	base := pawl.MetricSample{Value: 2, Breakdown: map[string]float64{"a.ts:1": 1}}
	cur := pawl.MetricSample{Value: 2, Breakdown: map[string]float64{"a.ts:1": 1, "a.ts:9": 1, "b.ts:3": 1}}

	got := pawl.GitHubAnnotations("cc", "Cognitive complexity", spec, base, cur)

	want := []string{
		"::error file=a.ts,line=9,title=pawl: cc::Cognitive complexity: new cc offender",
		"::error file=b.ts,line=3,title=pawl: cc::Cognitive complexity: new cc offender",
	}
	assertAnnotationsExact(t, got, want)
}

// A file's offender-count is the number of distinct keys, not a sum of
// values: a key present in both base and cur that merely changed value must
// not be treated as a new offender, even though the raw number moved.
func TestGitHubAnnotationsPerFileCountValueChangeAloneNotNewOffender(t *testing.T) {
	spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerFileCount}
	base := pawl.MetricSample{Value: 2, Breakdown: map[string]float64{"a.go:1": 1, "a.go:2": 1}}
	cur := pawl.MetricSample{Value: 2, Breakdown: map[string]float64{"a.go:1": 5, "a.go:2": 1}}

	got := pawl.GitHubAnnotations("cc", "Cognitive complexity", spec, base, cur)

	assertAnnotationsExact(t, got, nil)
}

// Builtins may key by file only, with no ":line" suffix. Splitting such a
// key at its (absent) first colon must yield file=<key> with no ",line="
// clause, never a spurious empty line value.
func TestGitHubAnnotationsPerFileCountKeyWithoutLine(t *testing.T) {
	spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerFileCount}
	base := pawl.MetricSample{Value: 0, Breakdown: map[string]float64{}}
	cur := pawl.MetricSample{Value: 1, Breakdown: map[string]float64{"c.go": 1}}

	got := pawl.GitHubAnnotations("dup", "Duplication", spec, base, cur)

	want := []string{
		"::error file=c.go,title=pawl: dup::Duplication: new dup offender",
	}
	assertAnnotationsExact(t, got, want)
}

// per-key-value: a key present in both samples is annotated only when it
// worsened past tolerance. Exactly at the tolerance boundary, and any
// improvement, must stay silent — the boundary is inclusive of "ok".
func TestGitHubAnnotationsPerKeyValueToleranceBoundary(t *testing.T) {
	spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerKeyValue, Tolerance: 1}
	base := pawl.MetricSample{Value: 21, Breakdown: map[string]float64{"a.go:5": 3, "b.go:9": 10, "c.go:2": 8}}
	cur := pawl.MetricSample{Value: 21, Breakdown: map[string]float64{"a.go:5": 5, "b.go:9": 11, "c.go:2": 6}}

	got := pawl.GitHubAnnotations("pk", "Per key", spec, base, cur)

	want := []string{
		"::error file=a.go,line=5,title=pawl: pk::Per key: 3 → 5",
	}
	assertAnnotationsExact(t, got, want)
}

// The total gate emits exactly one file-less annotation when the scalar
// worsened beyond tolerance.
func TestGitHubAnnotationsTotalGateRegressed(t *testing.T) {
	spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GateTotal}
	base := pawl.MetricSample{Value: 10}
	cur := pawl.MetricSample{Value: 12}

	got := pawl.GitHubAnnotations("cc", "Cognitive complexity", spec, base, cur)

	want := []string{
		"::error title=pawl: cc::Cognitive complexity regressed: 10 → 12",
	}
	assertAnnotationsExact(t, got, want)
}

// The total gate is silent when the scalar did not worsen.
func TestGitHubAnnotationsTotalGateNotRegressed(t *testing.T) {
	spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GateTotal}
	base := pawl.MetricSample{Value: 10}
	cur := pawl.MetricSample{Value: 9}

	got := pawl.GitHubAnnotations("cc", "Cognitive complexity", spec, base, cur)

	assertAnnotationsExact(t, got, nil)
}

// higher-is-better total: a drop is a regression, a rise is an improvement —
// the arrow direction in the message always reads base → cur regardless of
// which direction is "better".
func TestGitHubAnnotationsHigherIsBetterTotal(t *testing.T) {
	spec := pawl.GateSpec{Direction: pawl.HigherIsBetter, Gate: pawl.GateTotal}

	t.Run("drop regresses", func(t *testing.T) {
		base := pawl.MetricSample{Value: 90}
		cur := pawl.MetricSample{Value: 85}
		got := pawl.GitHubAnnotations("cov", "Coverage", spec, base, cur)
		want := []string{
			"::error title=pawl: cov::Coverage regressed: 90 → 85",
		}
		assertAnnotationsExact(t, got, want)
	})

	t.Run("rise is silent", func(t *testing.T) {
		base := pawl.MetricSample{Value: 85}
		cur := pawl.MetricSample{Value: 90}
		got := pawl.GitHubAnnotations("cov", "Coverage", spec, base, cur)
		assertAnnotationsExact(t, got, nil)
	})
}

// Output order is a deterministic sort by breakdown key, not map iteration
// order or discovery order.
func TestGitHubAnnotationsPerFileCountSortedByKey(t *testing.T) {
	spec := pawl.GateSpec{Direction: pawl.LowerIsBetter, Gate: pawl.GatePerFileCount}
	base := pawl.MetricSample{Value: 0, Breakdown: map[string]float64{}}
	cur := pawl.MetricSample{Value: 3, Breakdown: map[string]float64{"z.go:1": 1, "a.go:1": 1, "m.go:1": 1}}

	got := pawl.GitHubAnnotations("ord", "Ordering", spec, base, cur)

	want := []string{
		"::error file=a.go,line=1,title=pawl: ord::Ordering: new ord offender",
		"::error file=m.go,line=1,title=pawl: ord::Ordering: new ord offender",
		"::error file=z.go,line=1,title=pawl: ord::Ordering: new ord offender",
	}
	assertAnnotationsExact(t, got, want)
}

func assertAnnotationsExact(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("annotations = %#v, want %#v", got, want)
	}
}
