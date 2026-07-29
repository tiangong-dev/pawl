package pawl

import "testing"

func TestSinceKeepsPartiallyUnlocatedPerFileRegressionEnforcedInFull(t *testing.T) {
	base := Metric{Value: 1, Breakdown: map[string]float64{"a.go:5": 1}}
	cur := Metric{Value: 2, Breakdown: map[string]float64{"a.go:5": 1}} // one finding has no location
	current := cur.Value
	metric := MetricReport{
		ID:      "sarif",
		Base:    floatPtr(base.Value),
		Current: &current,
		Status:  "worse",
		Regressions: StructuredRegressions(
			GateSpec{Direction: LowerIsBetter, Gate: GatePerFileCount},
			MetricSample{Value: base.Value, Breakdown: base.Breakdown},
			MetricSample{Value: cur.Value, Breakdown: cur.Breakdown},
		),
	}
	scope := &sinceScope{}
	scope.scopeMetric(&metric, Dimension{
		ID: "sarif", Direction: LowerIsBetter, Gate: GatePerFileCount,
	}, base, cur)

	if len(scope.enforcedFull) != 1 || scope.enforcedFull[0] != "sarif" {
		t.Fatalf("enforcedFull = %v, want [sarif]", scope.enforcedFull)
	}
	if len(metric.Regressions) == 0 || metric.Regressions[0].Suppressed {
		t.Fatalf("unlocated scalar regression was suppressed: %+v", metric.Regressions)
	}
}
