package pawl

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestCoverageFloorsRejectDanglingSymlinksToOutside(t *testing.T) {
	for _, tc := range []struct {
		name     string
		linkPath string
		relPath  string
		target   string
	}{
		{name: "dangling file symlink", linkPath: "link.go", relPath: "link.go", target: "missing.go"},
		{name: "dangling intermediate directory symlink", linkPath: "linked", relPath: "linked/a.go", target: "missing-dir"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "repo")
			if err := os.Mkdir(root, 0o755); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(parent, "outside")
			if err := os.Mkdir(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, tc.linkPath)
			target := filepath.Join(outside, tc.target)
			relTarget, err := filepath.Rel(filepath.Dir(link), target)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(relTarget, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}

			lcovReport := "SF:" + tc.relPath + "\nLF:10\nLH:5\nend_of_record\n"
			writeCoverageTestReport(t, root, "coverage.info", lcovReport)
			t.Run("LCOV min_files", func(t *testing.T) {
				dim := coverageTestDimension("coverage.info", "lcov")
				_, err := measureCoverage(&Config{Dir: root}, dim, io.Discard)
				assertDanglingSymlinkRejected(t, err)
			})
			t.Run("LCOV min_found", func(t *testing.T) {
				dim := coverageTestDimension("coverage.info", "lcov")
				dim.Options["min_found"] = 10
				_, err := measureCoverage(&Config{Dir: root}, dim, io.Discard)
				assertDanglingSymlinkRejected(t, err)
			})

			xmlReport := `<coverage line-rate="0.5" lines-valid="10"><packages><package><classes><class filename="` + tc.relPath + `"/></classes></package></packages></coverage>`
			writeCoverageTestReport(t, root, "coverage.xml", xmlReport)
			t.Run("Cobertura min_files", func(t *testing.T) {
				_, err := measureCoverage(&Config{Dir: root}, coverageTestDimension("coverage.xml", "cobertura"), io.Discard)
				assertDanglingSymlinkRejected(t, err)
			})
		})
	}
}

func TestCoverageFloorsKeepOrdinaryMissingPaths(t *testing.T) {
	root := t.TempDir()
	writeCoverageTestReport(t, root, "coverage.info", "SF:missing.go\nLF:10\nLH:5\nend_of_record\n")
	lcov := coverageTestDimension("coverage.info", "lcov")
	lcov.Options["min_found"] = 10
	if _, err := measureCoverage(&Config{Dir: root}, lcov, io.Discard); err != nil {
		t.Fatalf("ordinary missing LCOV source should retain existing floor behavior: %v", err)
	}
	writeCoverageTestReport(t, root, "coverage.xml", `<coverage line-rate="0.5" lines-valid="10"><packages><package><classes><class filename="missing.go"/></classes></package></packages></coverage>`)
	if _, err := measureCoverage(&Config{Dir: root}, coverageTestDimension("coverage.xml", "cobertura"), io.Discard); err != nil {
		t.Fatalf("ordinary missing Cobertura source should retain existing floor behavior: %v", err)
	}
}

func TestCoverageFloorsRejectDanglingSymlinkToMissingInternalTarget(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("missing.go", filepath.Join(root, "link.go")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	writeCoverageTestReport(t, root, "coverage.info", "SF:link.go\nLF:10\nLH:5\nend_of_record\n")
	if _, err := measureCoverage(&Config{Dir: root}, coverageTestDimension("coverage.info", "lcov"), io.Discard); err == nil || !strings.Contains(err.Error(), "unresolved symlink") {
		t.Fatalf("LCOV internal dangling symlink error = %v, want fail-closed resolution error", err)
	}
	writeCoverageTestReport(t, root, "coverage.xml", `<coverage line-rate="0.5" lines-valid="10"><packages><package><classes><class filename="link.go"/></classes></package></packages></coverage>`)
	if _, err := measureCoverage(&Config{Dir: root}, coverageTestDimension("coverage.xml", "cobertura"), io.Discard); err == nil || !strings.Contains(err.Error(), "unresolved symlink") {
		t.Fatalf("Cobertura internal dangling symlink error = %v, want fail-closed resolution error", err)
	}
}

func TestCoverageFloorsFollowTwoHopSymlinkChains(t *testing.T) {
	t.Run("inside config root", func(t *testing.T) {
		root := t.TempDir()
		realDir := filepath.Join(root, "real")
		if err := os.Mkdir(realDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(realDir, "a.go"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("real", filepath.Join(root, "first")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if err := os.Symlink("first", filepath.Join(root, "second")); err != nil {
			t.Skipf("second symlink unavailable: %v", err)
		}
		writeCoverageTestReport(t, root, "coverage.info", "SF:second/a.go\nLF:10\nLH:5\nend_of_record\n")
		if _, err := measureCoverage(&Config{Dir: root}, coverageTestDimension("coverage.info", "lcov"), io.Discard); err != nil {
			t.Fatalf("two-hop in-root LCOV symlink should satisfy min_files: %v", err)
		}
	})

	t.Run("outside config root", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "repo")
		outside := filepath.Join(parent, "outside")
		realDir := filepath.Join(outside, "real")
		if err := os.MkdirAll(realDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(realDir, "a.go"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("real", filepath.Join(outside, "second")); err != nil {
			t.Skipf("first symlink unavailable: %v", err)
		}
		if err := os.Symlink(filepath.Join("..", "outside", "second"), filepath.Join(root, "first")); err != nil {
			t.Skipf("second symlink unavailable: %v", err)
		}
		writeCoverageTestReport(t, root, "coverage.info", "SF:first/a.go\nLF:10\nLH:5\nend_of_record\n")
		if _, err := measureCoverage(&Config{Dir: root}, coverageTestDimension("coverage.info", "lcov"), io.Discard); err == nil || !strings.Contains(err.Error(), "no valid source file evidence") {
			t.Fatalf("two-hop external LCOV symlink error = %v, want file-floor failure", err)
		}
	})
}

func TestCoverageFloorsRejectSymlinkLoops(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("loop.go", filepath.Join(root, "loop.go")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	writeCoverageTestReport(t, root, "coverage.info", "SF:loop.go\nLF:10\nLH:5\nend_of_record\n")
	dim := coverageTestDimension("coverage.info", "lcov")
	if _, err := measureCoverage(&Config{Dir: root}, dim, io.Discard); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("LCOV symlink-loop error = %v, want fail-closed resolution error", err)
	}
}

func TestCoveragePathResolutionPropagatesLstatPermissionErrors(t *testing.T) {
	root := t.TempDir()
	resolver := newCoveragePathResolver()
	resolver.lstat = func(path string) (os.FileInfo, error) {
		return nil, &os.PathError{Op: "lstat", Path: path, Err: syscall.EACCES}
	}
	if _, err := coverageSourcePathsWithResolver(root, nil, "missing.go", resolver); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Lstat permission error = %v, want propagated measurement error", err)
	}
}

func assertDanglingSymlinkRejected(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("measurement error = %v, want unresolved symlink failure", err)
	}
}
