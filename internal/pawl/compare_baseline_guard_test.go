package pawl_test

import (
	"reflect"
	"sort"
	"testing"

	pawl "github.com/tiangong-dev/pawl/internal/pawl"
)

func f64(v float64) *float64 { return &v }

// BaselineGuardViolations compares two recorded snapshots directly (not
// against a fresh measurement): a metric that worsened per its own recorded
// direction and tolerance is a violation; a metric dropped from pr is
// "removed" (legitimate, not a violation); a metric only in pr is ignored
// (a newly added dimension has no baseline to violate).
func TestBaselineGuardViolations(t *testing.T) {
	t.Run("a worsened metric is a violation", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 5}}
		pr := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 7}}
		violations, removed := pawl.BaselineGuardViolations(base, pr)
		wantLines(t, violations, []string{"a: 5 → 7"})
		wantLines(t, removed, nil)
	})

	t.Run("an unchanged or improved metric is not a violation", func(t *testing.T) {
		base := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 5}}
		pr := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 5}}
		violations, _ := pawl.BaselineGuardViolations(base, pr)
		wantLines(t, violations, nil)

		pr2 := map[string]pawl.Metric{"a": {Direction: pawl.LowerIsBetter, Value: 3}}
		violations2, _ := pawl.BaselineGuardViolations(base, pr2)
		wantLines(t, violations2, nil)
	})

	t.Run("empty direction defaults to lower-is-better", func(t *testing.T) {
		base := map[string]pawl.Metric{"b": {Direction: "", Value: 5}}
		pr := map[string]pawl.Metric{"b": {Direction: "", Value: 7}}
		violations, _ := pawl.BaselineGuardViolations(base, pr)
		wantLines(t, violations, []string{"b: 5 → 7"})
	})

	t.Run("recorded tolerance is honored", func(t *testing.T) {
		base := map[string]pawl.Metric{"c": {Direction: pawl.LowerIsBetter, Value: 5, Tolerance: f64(2)}}
		within := map[string]pawl.Metric{"c": {Direction: pawl.LowerIsBetter, Value: 7, Tolerance: f64(2)}}
		violations, _ := pawl.BaselineGuardViolations(base, within)
		wantLines(t, violations, nil)

		beyond := map[string]pawl.Metric{"c": {Direction: pawl.LowerIsBetter, Value: 8, Tolerance: f64(2)}}
		violations2, _ := pawl.BaselineGuardViolations(base, beyond)
		wantLines(t, violations2, []string{"c: 5 → 8"})
	})

	t.Run("higher-is-better honors its own direction", func(t *testing.T) {
		base := map[string]pawl.Metric{"h": {Direction: pawl.HigherIsBetter, Value: 10}}
		worse := map[string]pawl.Metric{"h": {Direction: pawl.HigherIsBetter, Value: 8}}
		violations, _ := pawl.BaselineGuardViolations(base, worse)
		wantLines(t, violations, []string{"h: 10 → 8"})

		better := map[string]pawl.Metric{"h": {Direction: pawl.HigherIsBetter, Value: 12}}
		violations2, _ := pawl.BaselineGuardViolations(base, better)
		wantLines(t, violations2, nil)
	})

	t.Run("a metric missing from pr is removed, not a violation", func(t *testing.T) {
		base := map[string]pawl.Metric{"d": {Direction: pawl.LowerIsBetter, Value: 5}}
		pr := map[string]pawl.Metric{}
		violations, removed := pawl.BaselineGuardViolations(base, pr)
		wantLines(t, violations, nil)
		wantLines(t, removed, []string{"d"})
	})

	t.Run("a metric only in pr is ignored entirely", func(t *testing.T) {
		base := map[string]pawl.Metric{}
		pr := map[string]pawl.Metric{"e": {Direction: pawl.LowerIsBetter, Value: 100}}
		violations, removed := pawl.BaselineGuardViolations(base, pr)
		wantLines(t, violations, nil)
		wantLines(t, removed, nil)
	})

	t.Run("multiple violations and removals are each sorted by id", func(t *testing.T) {
		base := map[string]pawl.Metric{
			"z": {Direction: pawl.LowerIsBetter, Value: 10},
			"a": {Direction: pawl.LowerIsBetter, Value: 10},
			"y": {Direction: pawl.LowerIsBetter, Value: 10},
			"b": {Direction: pawl.LowerIsBetter, Value: 10},
		}
		pr := map[string]pawl.Metric{
			"z": {Direction: pawl.LowerIsBetter, Value: 20},
			"a": {Direction: pawl.LowerIsBetter, Value: 20},
		}
		violations, removed := pawl.BaselineGuardViolations(base, pr)
		wantLines(t, violations, []string{"a: 10 → 20", "z: 10 → 20"})
		wantLines(t, removed, []string{"b", "y"})
	})
}

func wantLines(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(got)
	sort.Strings(want)
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %#v, want %#v", got, want)
	}
}
