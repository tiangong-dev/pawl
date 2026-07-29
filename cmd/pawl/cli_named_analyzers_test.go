package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamedEslintAnalyzerRunsOnceForMultipleDimensions(t *testing.T) {
	dir := t.TempDir()
	report, _ := json.Marshal([]eslintFileResult{{
		FilePath: filepath.Join(dir, "src", "a.ts"),
		Messages: []eslintMessage{
			{RuleID: "rule-a", Line: 10},
			{RuleID: "rule-b", Line: 10},
		},
	}})
	writeFile(t, dir, "eslint.json", string(report))
	writeFile(t, dir, "pawl.yaml", `
analyzers:
  - id: "frontend"
    builtin: "eslint"
    options:
      command: 'echo run >> "$PAWL_ROOT/runs"; cat "$PAWL_ROOT/eslint.json"'
      min_files: 1
dimensions:
  - id: "a"
    title: "Rule A"
    direction: "lower-is-better"
    gate: "per-file-count"
    source: "frontend"
    options:
      rules: ["rule-a"]
  - id: "b"
    title: "Rule B"
    direction: "lower-is-better"
    gate: "per-file-count"
    source: "frontend"
    options:
      rules: ["rule-b"]
`)

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if runs := strings.Count(readFile(t, filepath.Join(dir, "runs")), "run\n"); runs != 1 {
		t.Fatalf("analyzer executions = %d, want 1", runs)
	}
	snapshot := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	if snapshot.Metrics["a"].Value != 1 || snapshot.Metrics["b"].Value != 1 {
		t.Fatalf("metrics = %+v, want independently filtered values of 1", snapshot.Metrics)
	}
}

func TestRecordOnlyRunsSharedAnalyzerOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "eslint.json", `[{"filePath":"src/a.ts","messages":[
{"ruleId":"rule-a","line":1},{"ruleId":"rule-b","line":2}
]}]`)
	writeFile(t, dir, "pawl.yaml", `
analyzers:
  - id: "frontend"
    builtin: "eslint"
    options:
      command: 'echo run >> "$PAWL_ROOT/runs"; cat "$PAWL_ROOT/eslint.json"'
dimensions:
  - id: "a"
    title: "A"
    direction: "lower-is-better"
    source: "frontend"
    options: { rules: ["rule-a"] }
  - id: "b"
    title: "B"
    direction: "lower-is-better"
    source: "frontend"
    options: { rules: ["rule-b"] }
`)

	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("initial record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	res := runPawl(t, dir, baseEnv(), "record", "--only", "a,b")
	if res.exit != 0 {
		t.Fatalf("partial record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if runs := strings.Count(readFile(t, filepath.Join(dir, "runs")), "run\n"); runs != 2 {
		t.Fatalf("total analyzer executions = %d, want one full + one partial", runs)
	}
}

func TestNamedEslintAnalyzerVerifyRejectsMissingOrDisabledRule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "eslint-config.json", `{"rules":{"enabled-rule":[2,{}],"disabled-rule":"off"}}`)
	writeFile(t, dir, "eslint.json", `[]`)
	writeFile(t, dir, "pawl.yaml", `
analyzers:
  - id: "frontend"
    builtin: "eslint"
    verify:
      - 'cat "$PAWL_ROOT/eslint-config.json"'
    options:
      command: 'echo scanned >> "$PAWL_ROOT/runs"; cat "$PAWL_ROOT/eslint.json"'
dimensions:
  - id: "bad-rule"
    title: "Bad rule"
    direction: "lower-is-better"
    source: "frontend"
    options:
      rules: ["disabled-rule"]
`)

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, `configured rule "disabled-rule" is missing or disabled`) {
		t.Fatalf("stderr does not explain rule wiring failure: %s", res.stderr)
	}
	if got := readOptionalFile(filepath.Join(dir, "runs")); got != "" {
		t.Fatalf("lint scan ran after verification failed: %q", got)
	}
}

func TestNamedEslintAnalyzerVerifyAllowsCleanZero(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "eslint-config.json", `{"rules":{"enabled-rule":"error"}}`)
	writeFile(t, dir, "eslint.json", `[{"filePath":"src/clean.ts","messages":[]}]`)
	writeFile(t, dir, "pawl.yaml", `
analyzers:
  - id: "frontend"
    builtin: "eslint"
    verify:
      - 'cat "$PAWL_ROOT/eslint-config.json"'
    options:
      command: 'cat "$PAWL_ROOT/eslint.json"'
      min_files: 1
dimensions:
  - id: "clean"
    title: "Clean rule"
    direction: "lower-is-better"
    source: "frontend"
    options:
      rules: ["enabled-rule"]
`)

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if got := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["clean"].Value; got != 0 {
		t.Fatalf("clean value = %v, want 0", got)
	}
}

func TestNamedSarifAnalyzerCommandAndFileRunsOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "source.sarif", `{"runs":[{"artifacts":[{"location":{"uri":"a.go"}}],"results":[
{"ruleId":"alpha","level":"warning","locations":[{"physicalLocation":{"artifactLocation":{"uri":"a.go"},"region":{"startLine":4}}}]},
{"ruleId":"beta","level":"error","locations":[{"physicalLocation":{"artifactLocation":{"uri":"a.go"},"region":{"startLine":4}}}]}
]}]}`)
	writeFile(t, dir, "pawl.yaml", `
analyzers:
  - id: "go-lint"
    builtin: "sarif"
    options:
      command: 'echo run >> "$PAWL_ROOT/runs"; cp "$PAWL_ROOT/source.sarif" "$PAWL_ROOT/out.sarif"; exit 1'
      file: "out.sarif"
      min_files: 1
      valid_exit_codes: [0, 1]
dimensions:
  - id: "alpha"
    title: "Alpha"
    direction: "lower-is-better"
    gate: "per-file-count"
    source: "go-lint"
    options:
      rules: ["alpha"]
  - id: "errors"
    title: "Errors"
    direction: "lower-is-better"
    gate: "per-file-count"
    source: "go-lint"
    options:
      levels: ["error"]
`)

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if runs := strings.Count(readFile(t, filepath.Join(dir, "runs")), "run\n"); runs != 1 {
		t.Fatalf("analyzer executions = %d, want 1", runs)
	}
}

func TestNamedSarifAnalyzerValidatesRuleCatalogWithoutRejectingCleanZero(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "clean.sarif", `{"runs":[{"tool":{"driver":{"rules":[{"id":"known-rule"}]}},"artifacts":[{"location":{"uri":"a.go"}}],"results":[]}]}`)
	writeFile(t, dir, "pawl.yaml", `
analyzers:
  - id: "lint"
    builtin: "sarif"
    options:
      file: "clean.sarif"
      min_files: 1
      verify_rules: true
dimensions:
  - id: "known"
    title: "Known rule"
    direction: "lower-is-better"
    source: "lint"
    options:
      rules: ["known-rule"]
`)

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if got := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["known"].Value; got != 0 {
		t.Fatalf("clean value = %v, want 0", got)
	}
}

func TestNamedSarifAnalyzerRejectsUnknownRuleAndFatalExit(t *testing.T) {
	t.Run("unknown rule", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "report.sarif", `{"runs":[{"tool":{"driver":{"rules":[{"id":"known-rule"}]}},"results":[]}]}`)
		writeFile(t, dir, "pawl.yaml", `
analyzers:
  - id: "lint"
    builtin: "sarif"
    options:
      file: "report.sarif"
      verify_rules: true
dimensions:
  - id: "typo"
    title: "Typo"
    direction: "lower-is-better"
    source: "lint"
    options:
      rules: ["unknown-rule"]
`)

		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 2 || !strings.Contains(res.stderr, `"unknown-rule" is missing from the SARIF rule catalog`) {
			t.Fatalf("record = exit %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})

	t.Run("fatal exit", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "source.sarif", `{"runs":[]}`)
		writeFile(t, dir, "pawl.yaml", `
analyzers:
  - id: "lint"
    builtin: "sarif"
    options:
      command: 'cp "$PAWL_ROOT/source.sarif" "$PAWL_ROOT/out.sarif"; exit 2'
      file: "out.sarif"
      valid_exit_codes: [0, 1]
dimensions:
  - id: "all"
    title: "All findings"
    direction: "lower-is-better"
    source: "lint"
`)

		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 2 || !strings.Contains(res.stderr, "command exited with code 2") {
			t.Fatalf("record = exit %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})

	t.Run("verification without selector", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "report.sarif", `{"runs":[{"tool":{"driver":{"rules":[]}},"results":[]}]}`)
		writeFile(t, dir, "pawl.yaml", `
analyzers:
  - id: "lint"
    builtin: "sarif"
    options:
      file: "report.sarif"
      verify_rules: true
dimensions:
  - id: "all"
    title: "All"
    direction: "lower-is-better"
    source: "lint"
`)

		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 2 || !strings.Contains(res.stderr, "verify_rules requires at least one") {
			t.Fatalf("record = exit %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})
}

func TestNamedAnalyzerMinFilesFailsClosed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "eslint.json", `[{"filePath":"src/a.ts","messages":[]}]`)
	writeFile(t, dir, "pawl.yaml", `
analyzers:
  - id: "frontend"
    builtin: "eslint"
    options:
      command: 'cat "$PAWL_ROOT/eslint.json"'
      min_files: 2
dimensions:
  - id: "all"
    title: "All"
    direction: "lower-is-better"
    source: "frontend"
`)

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 || !strings.Contains(res.stderr, "scanned 1 file(s), expected at least 2") {
		t.Fatalf("record = exit %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

func TestNamedAnalyzerRejectsMisplacedOrUnknownOptions(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{
			name: "selector on analyzer",
			config: `
analyzers:
  - id: "lint"
    builtin: "eslint"
    options:
      command: "echo '[]'"
      rules: ["rule-a"]
dimensions:
  - id: "all"
    title: "All"
    direction: "lower-is-better"
    source: "lint"
`,
			want: `option "rules" is not valid for a named eslint analyzer`,
		},
		{
			name: "misspelled dimension selector",
			config: `
analyzers:
  - id: "lint"
    builtin: "sarif"
    options:
      file: "report.sarif"
dimensions:
  - id: "selected"
    title: "Selected"
    direction: "lower-is-better"
    source: "lint"
    options:
      rule: ["wanted"]
`,
			want: `option "rule" is not a valid sarif selector`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", tc.config)
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 || !strings.Contains(res.stderr, tc.want) {
				t.Fatalf("record = exit %d\nstdout=%s\nstderr=%s\nwant %q", res.exit, res.stdout, res.stderr, tc.want)
			}
		})
	}
}

func readOptionalFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
