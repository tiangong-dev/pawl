package main

// Integration tests for `pawl version` / `pawl --version`, per SPEC.md:
// both print exactly "pawl <version>\n" to stdout, exit 0, empty stderr,
// and must not read pawl.yaml. The version string defaults to "dev" and is
// overridden at build time via -ldflags "-X github.com/tiangong-dev/pawl/internal/pawl.Version=x.y.z".

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestVersionCommandPrintsDevByDefault(t *testing.T) {
	dir := t.TempDir() // no pawl.yaml present — version must not require config
	res := runPawl(t, dir, baseEnv(), "version")

	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (stdout=%q stderr=%q)", res.exit, res.stdout, res.stderr)
	}
	if res.stdout != "pawl dev\n" {
		t.Errorf("stdout = %q, want %q", res.stdout, "pawl dev\n")
	}
	if res.stderr != "" {
		t.Errorf("stderr = %q, want empty", res.stderr)
	}
}

func TestVersionFlagPrintsDevByDefault(t *testing.T) {
	dir := t.TempDir() // no pawl.yaml present — --version must not require config
	res := runPawl(t, dir, baseEnv(), "--version")

	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (stdout=%q stderr=%q)", res.exit, res.stdout, res.stderr)
	}
	if res.stdout != "pawl dev\n" {
		t.Errorf("stdout = %q, want %q", res.stdout, "pawl dev\n")
	}
	if res.stderr != "" {
		t.Errorf("stderr = %q, want empty", res.stderr)
	}
}

// TestVersionRespectsLdflagsOverride guards the exact ldflags variable path
// (github.com/tiangong-dev/pawl/internal/pawl.Version) that release builds inject the version
// through; a rename or relocation of that var silently breaks release
// versioning without this test noticing.
func TestVersionRespectsLdflagsOverride(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	binPath := filepath.Join(t.TempDir(), "pawl-versioned")
	build := exec.Command("go", "build",
		"-ldflags", "-X github.com/tiangong-dev/pawl/internal/pawl.Version=9.9.9",
		"-o", binPath, ".")
	build.Dir = pkgDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build with version ldflags failed: %v\n%s", err, out)
	}

	dir := t.TempDir() // no pawl.yaml present — version must not require config
	cmd := exec.Command(binPath, "version")
	cmd.Dir = dir
	cmd.Env = baseEnv()
	out, err := cmd.CombinedOutput()
	exit := cmd.ProcessState.ExitCode()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("running versioned pawl: %v\noutput=%s", err, out)
		}
	}

	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (output=%q)", exit, out)
	}
	if string(out) != "pawl 9.9.9\n" {
		t.Errorf("stdout = %q, want %q", out, "pawl 9.9.9\n")
	}
}
