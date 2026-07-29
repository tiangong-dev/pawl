package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNamedOxlintAnalyzerRunsOnceAndProjectsNativeJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oxlint.json", `{
  "diagnostics": [
    {
      "code": "eslint(no-debugger)",
      "severity": "error",
      "filename": "src/a.ts",
      "labels": [{"span": {"line": 5}}]
    },
    {
      "code": "typescript(no-explicit-any)",
      "severity": "warning",
      "filename": "src/a.ts",
      "labels": [{"span": {"line": 5}}]
    },
    {
      "severity": "error",
      "filename": "src/b.ts",
      "labels": [{"span": {"line": 2}}]
    }
  ],
  "number_of_files": 2,
  "number_of_rules": 95
}`)
	writeFile(t, dir, "oxlint-config.json", `{
  "rules": {
    "no-debugger": "error",
    "typescript/no-explicit-any": "warn"
  }
}`)
	writeFile(t, dir, "pawl.yaml", `
analyzers:
  - id: "frontend"
    builtin: "oxlint"
    verify:
      - 'cat "$PAWL_ROOT/oxlint-config.json"'
    options:
      command: 'echo run >> "$PAWL_ROOT/runs"; cat "$PAWL_ROOT/oxlint.json"; exit 1'
      min_files: 2
dimensions:
  - id: "debugger"
    title: "Debugger"
    direction: "lower-is-better"
    gate: "per-file-count"
    source: "frontend"
    options:
      rules: ["eslint(no-debugger)"]
  - id: "typescript-warnings"
    title: "TypeScript warnings"
    direction: "lower-is-better"
    gate: "per-file-count"
    source: "frontend"
    options:
      rules: ["typescript(no-explicit-any)"]
      levels: ["warning"]
  - id: "all"
    title: "All Oxlint diagnostics"
    direction: "lower-is-better"
    gate: "per-file-count"
    source: "frontend"
`)

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if runs := strings.Count(readFile(t, filepath.Join(dir, "runs")), "run\n"); runs != 1 {
		t.Fatalf("analyzer executions = %d, want 1", runs)
	}
	snapshot := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	if got := snapshot.Metrics["debugger"].Value; got != 1 {
		t.Fatalf("debugger value = %v, want 1", got)
	}
	if got := snapshot.Metrics["typescript-warnings"].Value; got != 1 {
		t.Fatalf("typescript warnings value = %v, want 1", got)
	}
	all := snapshot.Metrics["all"]
	if all.Value != 3 || all.Unit != "issues" {
		t.Fatalf("all metric = %+v, want value 3 and unit issues", all)
	}
	if all.Breakdown["src/a.ts:5"] != 2 || all.Breakdown["src/b.ts:2"] != 1 {
		t.Fatalf("all breakdown = %v, want same-line findings accumulated", all.Breakdown)
	}
}

func TestBuiltinOxlintSupportsFiltersAndFileURLs(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "src", "a.ts")
	writeFile(t, dir, "oxlint.json", `{
  "diagnostics": [
    {
      "code": "oxc(no-barrel-file)",
      "severity": "advice",
      "filename": "file://`+abs+`",
      "labels": [{"span": {"line": 7}}]
    },
    {
      "code": "eslint(no-debugger)",
      "severity": "error",
      "filename": "src/b.ts",
      "labels": [{"span": {"line": 3}}]
    }
  ],
  "number_of_files": 2
}`)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "oxlint-advice", direction: "lower-is-better", gate: "per-file-count",
		builtin: "oxlint",
		optionLines: []string{
			`command = 'cat "$PAWL_ROOT/oxlint.json"'`,
			`rules = ["oxc(no-barrel-file)"]`,
			`levels = ["advice"]`,
			`min_files = 2`,
		},
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	metric := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["oxlint-advice"]
	if metric.Value != 1 || metric.Breakdown["src/a.ts:7"] != 1 {
		t.Fatalf("metric = %+v, want one config-relative advice diagnostic", metric)
	}
}

func TestOxlintVerificationRejectsDisabledRuleBeforeScan(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oxlint-config.json", `{
  "rules": {
    "no-debugger": "allow",
    "unicorn/no-invalid-fetch-options": "warn"
  }
}`)
	writeFile(t, dir, "oxlint.json", `{"diagnostics":[],"number_of_files":1}`)
	writeFile(t, dir, "pawl.yaml", `
analyzers:
  - id: "frontend"
    builtin: "oxlint"
    verify:
      - 'cat "$PAWL_ROOT/oxlint-config.json"'
    options:
      command: 'echo scanned >> "$PAWL_ROOT/runs"; cat "$PAWL_ROOT/oxlint.json"'
dimensions:
  - id: "bad-rule"
    title: "Bad rule"
    direction: "lower-is-better"
    source: "frontend"
    options:
      rules: ["eslint(no-debugger)"]
`)

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, `configured rule "eslint(no-debugger)" is missing or disabled`) {
		t.Fatalf("stderr does not explain Oxlint rule wiring failure: %s", res.stderr)
	}
	if got := readOptionalFile(filepath.Join(dir, "runs")); got != "" {
		t.Fatalf("Oxlint scan ran after verification failed: %q", got)
	}
}

func TestOxlintVerificationAllowsPluginRuleCleanZero(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oxlint-config.json", `{
  "rules": {"unicorn/no-invalid-fetch-options": "warn"}
}`)
	writeFile(t, dir, "oxlint.json", `{"diagnostics":[],"number_of_files":1}`)
	writeFile(t, dir, "pawl.yaml", `
analyzers:
  - id: "frontend"
    builtin: "oxlint"
    verify:
      - 'cat "$PAWL_ROOT/oxlint-config.json"'
    options:
      command: 'cat "$PAWL_ROOT/oxlint.json"'
      min_files: 1
dimensions:
  - id: "fetch-options"
    title: "Invalid fetch options"
    direction: "lower-is-better"
    source: "frontend"
    options:
      rules: ["unicorn(no-invalid-fetch-options)"]
`)

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if got := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["fetch-options"].Value; got != 0 {
		t.Fatalf("clean value = %v, want 0", got)
	}
}

func TestOxlintNativeJSONFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		report string
		want   string
	}{
		{
			name:   "missing diagnostics",
			report: `{"number_of_files":1}`,
			want:   `missing "diagnostics" array`,
		},
		{
			name:   "missing number of files",
			report: `{"diagnostics":[]}`,
			want:   `missing "number_of_files"`,
		},
		{
			name:   "negative number of files",
			report: `{"diagnostics":[],"number_of_files":-1}`,
			want:   "number_of_files must be non-negative",
		},
		{
			name: "unknown severity",
			report: `{"diagnostics":[{"severity":"fatal","filename":"a.ts"}],
				"number_of_files":1}`,
			want: `unsupported severity "fatal"`,
		},
		{
			name: "more diagnostic files than scanned files",
			report: `{"diagnostics":[
				{"severity":"warning","filename":"a.ts"},
				{"severity":"warning","filename":"b.ts"}
			],"number_of_files":1}`,
			want: "diagnostics name 2 file(s), but number_of_files is 1",
		},
		{
			name: "negative label line",
			report: `{"diagnostics":[
				{"severity":"warning","filename":"a.ts","labels":[{"span":{"line":-1}}]}
			],"number_of_files":1}`,
			want: "negative label line",
		},
		{
			name:   "wrong format",
			report: `[{"filePath":"a.ts","messages":[]}]`,
			want:   "stdout is not Oxlint JSON",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "oxlint.json", tc.report)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "oxlint", direction: "lower-is-better", builtin: "oxlint",
				optionLines: []string{`command = 'cat "$PAWL_ROOT/oxlint.json"'`},
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 || !strings.Contains(res.stderr, tc.want) {
				t.Fatalf("record = exit %d\nstdout=%s\nstderr=%s\nwant %q", res.exit, res.stdout, res.stderr, tc.want)
			}
		})
	}
}

func TestOxlintExitTwoAndInvalidLevelFail(t *testing.T) {
	t.Run("exit two", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "oxlint.json", `{"diagnostics":[],"number_of_files":1}`)
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "oxlint", direction: "lower-is-better", builtin: "oxlint",
			optionLines: []string{`command = 'cat "$PAWL_ROOT/oxlint.json"; exit 2'`},
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 2 || !strings.Contains(res.stderr, "oxlint exited with fatal code 2") {
			t.Fatalf("record = exit %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})

	t.Run("invalid level", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "oxlint", direction: "lower-is-better", builtin: "oxlint",
			optionLines: []string{
				`command = "oxlint --format json"`,
				`levels = ["note"]`,
			},
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 2 || !strings.Contains(res.stderr, "error, warning or advice") {
			t.Fatalf("record = exit %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})
}

func TestNamedOxlintRejectsMisplacedOrUnknownSelectors(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{
			name: "selector on analyzer",
			config: `
analyzers:
  - id: "frontend"
    builtin: "oxlint"
    options:
      command: "oxlint --format json"
      levels: ["error"]
dimensions:
  - id: "all"
    title: "All"
    direction: "lower-is-better"
    source: "frontend"
`,
			want: `option "levels" is not valid for a named oxlint analyzer`,
		},
		{
			name: "misspelled dimension selector",
			config: `
analyzers:
  - id: "frontend"
    builtin: "oxlint"
    options:
      command: "oxlint --format json"
dimensions:
  - id: "errors"
    title: "Errors"
    direction: "lower-is-better"
    source: "frontend"
    options:
      severity: ["error"]
`,
			want: `option "severity" is not a valid oxlint selector`,
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
