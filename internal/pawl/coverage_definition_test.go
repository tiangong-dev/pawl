package pawl

import (
	"path/filepath"
	"testing"
)

func TestCoverageSourcePathsNormalizeAndStayWithinRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "src", "a.c")
	for _, source := range []string{"src/a.c", inside} {
		paths, err := coverageSourcePaths(root, nil, source)
		if err != nil {
			t.Fatalf("coverageSourcePaths(%q): %v", source, err)
		}
		if len(paths) != 1 || paths[0] != inside {
			t.Errorf("coverageSourcePaths(%q) = %v, want [%s]", source, paths, inside)
		}
	}
	for _, source := range []string{"../outside.c", filepath.Join(filepath.Dir(root), "outside.c")} {
		paths, err := coverageSourcePaths(root, nil, source)
		if err != nil {
			t.Fatalf("coverageSourcePaths(%q): %v", source, err)
		}
		if len(paths) != 0 {
			t.Errorf("coverageSourcePaths(%q) = %v, want no paths outside root", source, paths)
		}
	}
}

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
