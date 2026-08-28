package main

import (
	"strings"
	"testing"
	"time"
)

func TestArtifactMaxAgeHonorsSubsecondPrecision(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "junit.xml", junitMixedFixture)
	writeFile(t, dir, "pawl.yaml", `dimensions:
  - id: "tests"
    title: "Failures"
    direction: "lower-is-better"
    artifact_max_age: "2s"
    builtin: "junit"
    options:
      file: "junit.xml"
`)
	setMtime(t, dir, "junit.xml", time.Now().Add(-2100*time.Millisecond))

	res := runPawl(t, dir, baseEnv(), "record", "--format", "json")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 for a 2.1s-old artifact with a 2s limit\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

func TestArtifactMaxAgeCheckReportsFailedMetric(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "junit.xml", junitMixedFixture)
	writeFile(t, dir, "pawl.yaml", `dimensions:
  - id: "tests"
    title: "Failures"
    direction: "lower-is-better"
    builtin: "junit"
    options:
      file: "junit.xml"
`)
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("seed record exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	writeFile(t, dir, "pawl.yaml", `dimensions:
  - id: "tests"
    title: "Failures"
    direction: "lower-is-better"
    artifact_max_age: "1h"
    builtin: "junit"
    options:
      file: "junit.xml"
`)
	setMtime(t, dir, "junit.xml", time.Now().Add(-2*time.Hour))

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 2 {
		t.Fatalf("check exit = %d, want 2 for stale artifact\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	rep := parseReport(t, res.stdout)
	if rep.FailureClass != "could-not-measure" {
		t.Fatalf("failure_class = %q, want could-not-measure\nreport=%s", rep.FailureClass, res.stdout)
	}
	if !strings.Contains(rep.Error, "artifact") || !strings.Contains(rep.Error, "tests") {
		t.Errorf("error = %q, want artifact diagnostic naming tests", rep.Error)
	}
	if len(rep.FailedMetrics) != 1 || rep.FailedMetrics[0] != "tests" {
		t.Errorf("failed_metrics = %v, want [tests]", rep.FailedMetrics)
	}
}
