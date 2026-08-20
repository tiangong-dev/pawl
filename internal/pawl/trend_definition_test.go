package pawl

import (
	"strings"
	"testing"
)

func TestTrendLegacyDefinitionRemainsComparable(t *testing.T) {
	metrics := []trendMetric{{
		ID: "m", Direction: LowerIsBetter, Unit: "count",
		Points: []trendPoint{
			{Commit: "1111111", Date: "2026-01-01", Value: 5},
			{Commit: "2222222", Date: "2026-01-02", Value: 50, Definition: "v1:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
	}}
	var out strings.Builder
	renderTrendText(&out, metrics)
	if !strings.Contains(out.String(), "50  +45") || strings.Contains(out.String(), "redefined") {
		t.Fatalf("trend treated a legacy upgrade as a definition boundary:\n%s", out.String())
	}
}

func TestTrendMarksDefinitionBoundaryInsteadOfComputingDelta(t *testing.T) {
	metrics := []trendMetric{{
		ID: "m", Direction: LowerIsBetter, Unit: "count",
		Points: []trendPoint{
			{Commit: "1111111", Date: "2026-01-01", Value: 5, Definition: "old"},
			{Commit: "2222222", Date: "2026-01-02", Value: 50, Definition: "new"},
		},
	}}
	var out strings.Builder
	renderTrendText(&out, metrics)
	if !strings.Contains(out.String(), "50  redefined") {
		t.Fatalf("trend compared values across definitions:\n%s", out.String())
	}
}
