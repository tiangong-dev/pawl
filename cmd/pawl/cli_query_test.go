package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusRequiresSnapshot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "fl", direction: "lower-is-better", builtin: "file-length",
		optionLines: []string{`include = ["**/*.txt"]`, "threshold = 10"},
	}))
	res := runPawl(t, dir, baseEnv(), "status")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstderr=%s", res.exit, res.stderr)
	}
}

func TestStatusReadsSnapshotWithoutMeasuring(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", nLines(3))
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "fl", direction: "lower-is-better", builtin: "file-length",
		optionLines: []string{`include = ["**/*.txt"]`, "threshold = 1"},
	}))
	// Growing the file after record must not change status — it does not measure.
	writeFile(t, dir, "a.txt", nLines(50))

	res := runPawl(t, dir, baseEnv(), "status", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stderr, "measuring") {
		t.Errorf("status must not measure: stderr=%s", res.stderr)
	}
	var payload struct {
		Command string `json:"command"`
		Metrics []struct {
			ID    string  `json:"id"`
			Value float64 `json:"value"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
		t.Fatalf("status json: %v\n%s", err, res.stdout)
	}
	if payload.Command != "status" {
		t.Errorf("command = %q, want status", payload.Command)
	}
	if len(payload.Metrics) != 1 || payload.Metrics[0].ID != "fl" {
		t.Fatalf("metrics = %+v, want fl", payload.Metrics)
	}
	if payload.Metrics[0].Value != 1 {
		t.Errorf("status value = %v, want the recorded 1, not a re-measure", payload.Metrics[0].Value)
	}
}

func TestConstraintsJSONIncludesThresholdAndPattern(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{
			id: "fl", direction: "lower-is-better", builtin: "file-length",
			optionLines: []string{`include = ["**/*.go"]`, "threshold = 500"},
		},
		dimDef{
			id: "notes", direction: "lower-is-better", builtin: "pattern-count",
			optionLines: []string{`pattern = "NOTE"`, `include = ["**/*.go"]`},
		},
	))
	res := runPawl(t, dir, baseEnv(), "constraints", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, `"id": "fl"`) || !strings.Contains(res.stdout, "500") {
		t.Errorf("constraints json missing file-length threshold: %s", res.stdout)
	}
	if !strings.Contains(res.stdout, "NOTE") {
		t.Errorf("constraints json missing pattern: %s", res.stdout)
	}
}

func TestRankListsOverNearAndOk(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "over.txt", nLines(20))
	writeFile(t, dir, "near.txt", nLines(10))
	writeFile(t, dir, "ok.txt", nLines(2))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "fl", direction: "lower-is-better", builtin: "file-length",
		optionLines: []string{`include = ["**/*.txt"]`, "threshold = 11"},
	}))

	res := runPawl(t, dir, baseEnv(), "rank", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	var payload struct {
		Command    string `json:"command"`
		Dimensions []struct {
			ID        string `json:"id"`
			Threshold int    `json:"threshold"`
			Files     []struct {
				Path   string  `json:"path"`
				Value  float64 `json:"value"`
				Status string  `json:"status"`
			} `json:"files"`
		} `json:"dimensions"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
		t.Fatalf("rank json: %v\n%s", err, res.stdout)
	}
	if payload.Command != "rank" || len(payload.Dimensions) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	byPath := map[string]string{}
	for _, f := range payload.Dimensions[0].Files {
		byPath[f.Path] = f.Status
	}
	if byPath["over.txt"] != "over" {
		t.Errorf("over.txt status = %q, want over", byPath["over.txt"])
	}
	if byPath["near.txt"] != "near" {
		t.Errorf("near.txt status = %q, want near (10 > 0.9*11)", byPath["near.txt"])
	}
	if byPath["ok.txt"] != "ok" {
		t.Errorf("ok.txt status = %q, want ok", byPath["ok.txt"])
	}
}

func TestRankWithoutSizeDimensionExitsTwo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "notes", direction: "lower-is-better", builtin: "pattern-count",
		optionLines: []string{`pattern = "NOTE"`, `include = ["**/*.go"]`},
	}))
	res := runPawl(t, dir, baseEnv(), "rank")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstderr=%s", res.exit, res.stderr)
	}
}

func TestQueryCommandsRejectCodeclimate(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "fl", direction: "lower-is-better", builtin: "file-length",
		optionLines: []string{`include = ["**/*.txt"]`, "threshold = 10"},
	}))
	for _, cmd := range []string{"status", "constraints", "rank"} {
		res := runPawl(t, dir, baseEnv(), cmd, "--format", "codeclimate")
		if res.exit != 2 {
			t.Errorf("%s --format codeclimate exit = %d, want 2\nstderr=%s", cmd, res.exit, res.stderr)
		}
	}
}

func TestQueryCommandsRejectExtraOperand(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "fl", direction: "lower-is-better", builtin: "file-length",
		optionLines: []string{`include = ["**/*.txt"]`, "threshold = 10"},
	}))
	for _, cmd := range []string{"status", "constraints", "rank"} {
		res := runPawl(t, dir, baseEnv(), cmd, "extra")
		if res.exit != 2 {
			t.Errorf("%s extra exit = %d, want 2\nstderr=%s", cmd, res.exit, res.stderr)
		}
	}
}
