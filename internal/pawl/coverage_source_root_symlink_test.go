package pawl

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoverageAbsoluteSourceAliasInsideConfigRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "repo-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	source := filepath.Join(root, "src", "a.go")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	aliasSource := filepath.Join(alias, "src", "a.go")

	t.Run("LCOV absolute SF", func(t *testing.T) {
		writeCoverageTestReport(t, root, "coverage.info", "SF:"+aliasSource+"\nLF:10\nLH:5\nend_of_record\n")
		result, err := measureCoverage(&Config{Dir: root}, coverageTestDimension("coverage.info", "lcov"), io.Discard)
		if err != nil {
			t.Fatalf("LCOV report through in-root alias failed: %v", err)
		}
		if result.Value != 50 {
			t.Errorf("coverage = %v, want 50", result.Value)
		}
	})

	t.Run("Cobertura absolute filename", func(t *testing.T) {
		report := `<coverage line-rate="0.5" lines-valid="10"><packages><package><classes><class filename="` + aliasSource + `"/></classes></package></packages></coverage>`
		writeCoverageTestReport(t, root, "coverage.xml", report)
		result, err := measureCoverage(&Config{Dir: root}, coverageTestDimension("coverage.xml", "cobertura"), io.Discard)
		if err != nil {
			t.Fatalf("Cobertura report through in-root alias failed: %v", err)
		}
		if result.Value != 50 {
			t.Errorf("coverage = %v, want 50", result.Value)
		}
	})

	t.Run("Cobertura absolute source root with relative class", func(t *testing.T) {
		report := `<coverage line-rate="0.5" lines-valid="10"><sources><source>` + alias + `</source></sources><packages><package><classes><class filename="src/a.go"/></classes></package></packages></coverage>`
		writeCoverageTestReport(t, root, "coverage.xml", report)
		result, err := measureCoverage(&Config{Dir: root}, coverageTestDimension("coverage.xml", "cobertura"), io.Discard)
		if err != nil {
			t.Fatalf("Cobertura relative class through in-root source alias failed: %v", err)
		}
		if result.Value != 50 {
			t.Errorf("coverage = %v, want 50", result.Value)
		}
	})
}

func TestCoverageSymlinkOutsideConfigRootDoesNotSatisfyFileFloor(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "a.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "outside-alias")
	if err := os.Symlink(outside, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	t.Run("LCOV absolute SF", func(t *testing.T) {
		report := "SF:" + filepath.Join(alias, "a.go") + "\nLF:10\nLH:5\nend_of_record\n"
		writeCoverageTestReport(t, root, "coverage.info", report)
		_, err := measureCoverage(&Config{Dir: root}, coverageTestDimension("coverage.info", "lcov"), io.Discard)
		if err == nil || !strings.Contains(err.Error(), "no valid source file evidence") {
			t.Fatalf("LCOV outside alias error = %v, want file-floor evidence failure", err)
		}
	})

	t.Run("Cobertura absolute source root", func(t *testing.T) {
		report := `<coverage line-rate="0.5" lines-valid="10"><sources><source>` + alias + `</source></sources><packages><package><classes><class filename="a.go"/></classes></package></packages></coverage>`
		writeCoverageTestReport(t, root, "coverage.xml", report)
		_, err := measureCoverage(&Config{Dir: root}, coverageTestDimension("coverage.xml", "cobertura"), io.Discard)
		if err == nil || !strings.Contains(err.Error(), "no valid source file evidence") {
			t.Fatalf("Cobertura outside source root error = %v, want file-floor evidence failure", err)
		}
	})
}

func coverageTestDimension(file, format string) Dimension {
	return Dimension{
		ID:      "coverage",
		Builtin: builtinCoverage,
		Options: map[string]any{
			"file":      file,
			"format":    format,
			"metric":    "lines",
			"min_files": 1,
		},
	}
}

func writeCoverageTestReport(t *testing.T, root, file, report string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, file), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
}
