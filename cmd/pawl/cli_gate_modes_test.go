package main

import (
	"strings"
	"testing"
)

// per-file-count gating blocks a localized regression end-to-end even when
// the scalar total is unchanged: file A improves, file B worsens, the total
// stays flat, and the gate's per-file verdict — not the scalar-only status —
// must still fail check.
func TestCheckPerFileCountBlocksNetZeroMasking(t *testing.T) {
	dir := t.TempDir()
	base := buildConfig("", dimDef{
		id: "nolint", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 2, "breakdown": {"a.go:1": 1, "b.go:1": 1}}'`,
	})
	mustRecord(t, dir, base)

	shifted := buildConfig("", dimDef{
		id: "nolint", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 2, "breakdown": {"a.go:1": 1, "a.go:2": 1}}'`,
	})
	writeFile(t, dir, "pawl.yaml", shifted)

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 1 {
		t.Fatalf("check exit = %d, want 1 (net-zero total must not mask the per-file regression)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "❌ regressions:") {
		t.Errorf("stdout missing regressions block: %s", res.stdout)
	}
	if !strings.Contains(res.stdout, "a.go  1 → 2") {
		t.Errorf("stdout missing per-file detail line %q: %s", "a.go  1 → 2", res.stdout)
	}
	if strings.Contains(res.stdout, "total 2 → 2") {
		t.Errorf("stdout should not report a scalar regression when the total is unchanged: %s", res.stdout)
	}
}

// Tolerance never applies to per-file-count: a file gaining even one
// offender regresses regardless of the dimension's declared tolerance.
func TestCheckPerFileCountIgnoresTolerance(t *testing.T) {
	dir := t.TempDir()
	tol := 100.0
	base := buildConfig("", dimDef{
		id: "nolint", direction: "lower-is-better", gate: "per-file-count", tolerance: &tol,
		command: `echo '{"value": 1, "breakdown": {"a.go:1": 1}}'`,
	})
	mustRecord(t, dir, base)

	worse := buildConfig("", dimDef{
		id: "nolint", direction: "lower-is-better", gate: "per-file-count", tolerance: &tol,
		command: `echo '{"value": 2, "breakdown": {"a.go:1": 1, "a.go:2": 1}}'`,
	})
	writeFile(t, dir, "pawl.yaml", worse)

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 1 {
		t.Fatalf("check exit = %d, want 1 (per-file-count ignores tolerance)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
}
