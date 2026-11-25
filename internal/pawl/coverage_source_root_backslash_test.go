package pawl

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoberturaBackslashSourceRootAndAbsoluteClassShareFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "src", "pkg", "a.go")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	report := `<coverage line-rate="0.8" lines-valid="10"><packages><package><sources><source>src\pkg</source></sources><classes><class filename="a.go"/><class filename="` + source + `"/></classes></package></packages></coverage>`
	if err := os.WriteFile(filepath.Join(root, "coverage.xml"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	dim := Dimension{
		ID:      "coverage",
		Builtin: builtinCoverage,
		Options: map[string]any{
			"file":      "coverage.xml",
			"format":    "cobertura",
			"metric":    "lines",
			"min_files": 2,
		},
	}
	_, err := measureCoverage(&Config{Dir: root}, dim, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "contains 1 distinct valid source file") {
		t.Fatalf("measurement error = %v, want duplicate path to fail min_files with one distinct file", err)
	}
}

func TestCoberturaBackslashSourceRootAmbiguousDirectoriesFailClosed(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{filepath.Join(root, `src\pkg`), filepath.Join(root, "src", "pkg")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(dir), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	report := `<coverage line-rate="0.8" lines-valid="10"><packages><package><sources><source>src\pkg</source></sources><classes><class filename="a.go"/></classes></package></packages></coverage>`
	writeCoverageTestReport(t, root, "coverage.xml", report)
	_, err := measureCoverage(&Config{Dir: root}, coverageTestDimension("coverage.xml", "cobertura"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "ambiguous backslash path") {
		t.Fatalf("measurement error = %v, want ambiguity failure for distinct source-root variants", err)
	}
}

func TestCoberturaMultipleSourceRootsResolveOnlyUniqueExistingCandidate(t *testing.T) {
	t.Run("one existing source", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "src", "a.go")
		if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		report := `<coverage line-rate="0.8" lines-valid="10"><sources><source>src</source><source>lib</source></sources><packages><package><classes><class filename="a.go"/></classes></package></packages></coverage>`
		writeCoverageTestReport(t, root, "coverage.xml", report)
		result, err := measureCoverage(&Config{Dir: root}, coverageTestDimension("coverage.xml", "cobertura"), io.Discard)
		if err != nil {
			t.Fatalf("unique existing source should resolve: %v", err)
		}
		if result.Value != 80 {
			t.Errorf("coverage = %v, want 80", result.Value)
		}
	})

	t.Run("multiple different existing sources", func(t *testing.T) {
		root := t.TempDir()
		for _, dir := range []string{"src", "lib"} {
			source := filepath.Join(root, dir, "a.go")
			if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(source, []byte(dir), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		report := `<coverage line-rate="0.8" lines-valid="10"><sources><source>src</source><source>lib</source></sources><packages><package><classes><class filename="a.go"/></classes></package></packages></coverage>`
		writeCoverageTestReport(t, root, "coverage.xml", report)
		_, err := measureCoverage(&Config{Dir: root}, coverageTestDimension("coverage.xml", "cobertura"), io.Discard)
		if err == nil || !strings.Contains(err.Error(), "multiple existing source files") {
			t.Fatalf("measurement error = %v, want multiple-candidate failure", err)
		}
	})
}
