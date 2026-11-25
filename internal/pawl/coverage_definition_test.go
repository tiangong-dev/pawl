package pawl

import (
	"testing"
)

func TestCoverageCompletenessFloorsDoNotRedefineMeasurement(t *testing.T) {
	cfg := &Config{}
	dim := Dimension{
		ID:        "coverage",
		Direction: HigherIsBetter,
		Builtin:   builtinCoverage,
		Options: map[string]any{
			"file":   "coverage.info",
			"format": "lcov",
			"metric": "lines",
		},
	}
	baseline, err := definitionFingerprint(cfg, dim)
	if err != nil {
		t.Fatal(err)
	}
	if want := "v1:sha256:088b300db2bc5eaeac47cc24d068cb58f0bfd215d73fb92f21c5e7d698acb2d8"; baseline != want {
		t.Fatalf("baseline fingerprint = %s, want existing %s", baseline, want)
	}
	dim.Options["min_files"] = 12
	dim.Options["min_found"] = 1000
	withFloors, err := definitionFingerprint(cfg, dim)
	if err != nil {
		t.Fatal(err)
	}
	if withFloors != baseline {
		t.Errorf("completeness floors changed fingerprint: got %s, want %s", withFloors, baseline)
	}
}
