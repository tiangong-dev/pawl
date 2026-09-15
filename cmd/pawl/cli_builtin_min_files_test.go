package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const todoToken = "TO" + "DO"

// The native file primitives know exactly which regular files they opened, so
// min_files fails a zero-match scan instead of recording a misleading zero.
func TestNativeFileBuiltinsMinFilesRejectsEmptyScan(t *testing.T) {
	cases := []struct {
		name    string
		builtin string
		opts    []string
	}{
		{"file-length", "file-length", []string{"threshold = 1", `include = ["**/*.ts"]`, "min_files = 1"}},
		{"file-bytes", "file-bytes", []string{"threshold = 1", `include = ["**/*.ts"]`, "min_files = 1"}},
		{"pattern-count", "pattern-count", []string{`pattern = "` + todoToken + `"`, `include = ["**/*.ts"]`, "min_files = 1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "only.go", "package only\n")
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "native", direction: "lower-is-better", builtin: tc.builtin, optionLines: tc.opts,
			}))
			root, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatalf("resolve fixture root: %v", err)
			}

			res := runPawl(t, dir, baseEnv(), "measure")
			if res.exit != 2 {
				t.Fatalf("measure exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			wantFailure := fmt.Sprintf("scanned 0 file(s) under %s, expected at least 1", root)
			if !strings.Contains(res.stderr, wantFailure) {
				t.Fatalf("failure must name actual count, root, and required count %q:\n%s", wantFailure, res.stderr)
			}
		})
	}
}

// Counts apply after both include and exclude filtering, and successful scans
// announce their scope without adding operational metadata to the document.
func TestNativeFileBuiltinsMinFilesCountsFilteredRegularFilesAndProgress(t *testing.T) {
	cases := []struct {
		name    string
		builtin string
		opts    []string
	}{
		{"file-length", "file-length", []string{"threshold = 1", `include = ["**/*.txt"]`, `exclude = ["excluded/**"]`, "min_files = 2"}},
		{"file-bytes", "file-bytes", []string{"threshold = 1", `include = ["**/*.txt"]`, `exclude = ["excluded/**"]`, "min_files = 2"}},
		{"pattern-count", "pattern-count", []string{`pattern = "` + todoToken + `"`, `include = ["**/*.txt"]`, `exclude = ["excluded/**"]`, "min_files = 2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "one.txt", todoToken+"\n")
			writeFile(t, dir, "two.txt", todoToken+"\n")
			writeFile(t, dir, "excluded/three.txt", todoToken+"\n")
			writeFile(t, dir, "other.go", todoToken+"\n")
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "native", direction: "lower-is-better", builtin: tc.builtin, optionLines: tc.opts,
			}))

			res := runPawl(t, dir, baseEnv(), "measure")
			if res.exit != 0 {
				t.Fatalf("measure exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			root, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatalf("resolve fixture root: %v", err)
			}
			wantProgress := fmt.Sprintf("native scanned 2 file(s) under %s", root)
			if !strings.Contains(res.stderr, wantProgress) {
				t.Fatalf("stderr missing filtered scan progress %q:\n%s", wantProgress, res.stderr)
			}
			if strings.Contains(res.stdout, "scanned") || strings.Contains(res.stdout, dir) {
				t.Fatalf("measurement document must omit scan metadata:\n%s", res.stdout)
			}

			quiet := runPawl(t, dir, baseEnv(), "measure", "--quiet")
			if quiet.exit != 0 || quiet.stderr != "" {
				t.Fatalf("quiet measure must suppress progress: exit=%d stderr=%q", quiet.exit, quiet.stderr)
			}
		})
	}
}
