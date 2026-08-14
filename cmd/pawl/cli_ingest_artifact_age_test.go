package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A dimension configured with `file:` and no `command:` reads whatever is on
// disk. A report generated days ago parses fine and yields a number that looks
// as current as any other — the one thing the verdict never said was how old
// the evidence was. Every artifact-reading builtin must name the file it read
// and when that file was last written.

const lcovFixture80 = "SF:a.go\nLF:10\nLH:8\nend_of_record\n"

const sarifFixtureOneFinding = `{"runs":[{"artifacts":[{"location":{"uri":"a.go"}}],` +
	`"results":[{"ruleId":"r1","level":"error","locations":[{"physicalLocation":` +
	`{"artifactLocation":{"uri":"a.go"},"region":{"startLine":3}}}]}]}]}`

// artifactSource is one way a dimension can end up reading a file off disk.
// The set is the full list of readers that accept `file:` without requiring a
// `command:` — junit/sarif/coverage (readIngestReport), json-value, and a
// named sarif analyzer.
type artifactSource struct {
	name     string
	artifact string
	content  string
	config   string // pawl.yaml reading `artifact` with no command
	id       string // the dimension whose verdict carries the artifact
}

func fileOnlyArtifactSources() []artifactSource {
	return []artifactSource{
		{
			name: "junit", artifact: "junit.xml", content: junitMixedFixture, id: "tests",
			config: `dimensions:
  - id: "tests"
    title: "Failures"
    direction: "lower-is-better"
    builtin: "junit"
    options:
      file: "junit.xml"
`,
		},
		{
			name: "sarif", artifact: "report.sarif", content: sarifFixtureOneFinding, id: "findings",
			config: `dimensions:
  - id: "findings"
    title: "Findings"
    direction: "lower-is-better"
    builtin: "sarif"
    options:
      file: "report.sarif"
`,
		},
		{
			name: "coverage", artifact: "lcov.info", content: lcovFixture80, id: "cov",
			config: `dimensions:
  - id: "cov"
    title: "Coverage"
    direction: "higher-is-better"
    builtin: "coverage"
    options:
      file: "lcov.info"
      format: "lcov"
`,
		},
		{
			name: "json-value", artifact: "stats.json", content: `{"n": 42}`, id: "num",
			config: `dimensions:
  - id: "num"
    title: "Number"
    direction: "lower-is-better"
    builtin: "json-value"
    options:
      file: "stats.json"
      path: "n"
`,
		},
		{
			name: "named sarif analyzer", artifact: "lint.sarif", content: sarifFixtureOneFinding, id: "lint-findings",
			config: `analyzers:
  - id: "lint"
    builtin: "sarif"
    options:
      file: "lint.sarif"
dimensions:
  - id: "lint-findings"
    title: "Lint findings"
    direction: "lower-is-better"
    source: "lint"
`,
		},
	}
}

// setMtime backdates an artifact so the age the verdict reports is a known
// quantity rather than "however long this test took".
func setMtime(t *testing.T, dir, name string, when time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("chtimes %s: %v", name, err)
	}
}

func mustMetric(t *testing.T, rep jsonReport, id string) jsonMetric {
	t.Helper()
	m, ok := metricByID(rep, id)
	if !ok {
		t.Fatalf("no metric %q in report: %+v", id, rep.Metrics)
	}
	return m
}

// Every file-reading builtin reports the artifact it read: its path, its mtime,
// and how old it was at measurement time. generated is false — nothing in this
// invocation produced the file, so the number is only as fresh as the file.
func TestArtifactAgeReportedForFileOnlyDimensions(t *testing.T) {
	const backdate = 3 * time.Hour
	for _, src := range fileOnlyArtifactSources() {
		t.Run(src.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, src.artifact, src.content)
			writeFile(t, dir, "pawl.yaml", src.config)
			written := time.Now().Add(-backdate)
			setMtime(t, dir, src.artifact, written)

			if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
				t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
			if res.exit != 0 {
				t.Fatalf("check exit = %d, want 0 (artifact age must not change the verdict)\nstdout=%s\nstderr=%s",
					res.exit, res.stdout, res.stderr)
			}

			m := mustMetric(t, parseReport(t, res.stdout), src.id)
			if m.Artifact == nil {
				t.Fatalf("metric %q has no artifact — a file-read measurement must name what it read", src.id)
			}
			if m.Artifact.Path != src.artifact {
				t.Errorf("artifact.path = %q, want %q", m.Artifact.Path, src.artifact)
			}
			if m.Artifact.Generated {
				t.Errorf("artifact.generated = true, want false (no command produced this file)")
			}
			wantAge := int64(backdate.Seconds())
			if diff := m.Artifact.AgeSeconds - wantAge; diff < -120 || diff > 120 {
				t.Errorf("artifact.age_seconds = %d, want ≈%d", m.Artifact.AgeSeconds, wantAge)
			}
			mod, err := time.Parse(time.RFC3339Nano, m.Artifact.Modified)
			if err != nil {
				t.Fatalf("artifact.modified %q is not RFC3339: %v", m.Artifact.Modified, err)
			}
			if d := mod.Sub(written); d > 2*time.Second || d < -2*time.Second {
				t.Errorf("artifact.modified = %s, want ≈%s", mod, written)
			}
		})
	}
}

// When the dimension's own command produces the file, the artifact is this
// invocation's own output: generated is true and the age is ~0. Reporting it
// anyway keeps the field's meaning uniform — a consumer never has to know
// which builtins happen to write their own reports.
func TestArtifactMarkedGeneratedWhenCommandProducesIt(t *testing.T) {
	cases := []struct {
		name     string
		artifact string
		id       string
		config   string
	}{
		{
			name: "junit", artifact: "out.xml", id: "tests",
			config: `dimensions:
  - id: "tests"
    title: "Failures"
    direction: "lower-is-better"
    builtin: "junit"
    options:
      command: 'cp "$PAWL_ROOT/src.xml" "$PAWL_ROOT/out.xml"'
      file: "out.xml"
`,
		},
		{
			name: "json-value", artifact: "out.json", id: "num",
			config: `dimensions:
  - id: "num"
    title: "Number"
    direction: "lower-is-better"
    builtin: "json-value"
    options:
      command: 'echo "{\"n\": 42}" > "$PAWL_ROOT/out.json"'
      file: "out.json"
      path: "n"
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "src.xml", junitMixedFixture)
			writeFile(t, dir, "pawl.yaml", tc.config)

			if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
				t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
			if res.exit != 0 {
				t.Fatalf("check exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}

			m := mustMetric(t, parseReport(t, res.stdout), tc.id)
			if m.Artifact == nil {
				t.Fatalf("metric %q has no artifact", tc.id)
			}
			if m.Artifact.Path != tc.artifact {
				t.Errorf("artifact.path = %q, want %q", m.Artifact.Path, tc.artifact)
			}
			if !m.Artifact.Generated {
				t.Errorf("artifact.generated = false, want true (the dimension's command wrote this file)")
			}
			if m.Artifact.AgeSeconds > 120 {
				t.Errorf("artifact.age_seconds = %d, want ≈0 for a file this run produced", m.Artifact.AgeSeconds)
			}
		})
	}
}

// record writes the snapshot from the same measurement check compares against,
// so its verdict carries the same provenance — including the partial
// `record --only` form, where a preserved dimension has none because this
// invocation did not measure it.
func TestRecordJSONCarriesArtifactAndPreservedDimensionsDoNot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "junit.xml", junitMixedFixture)
	writeFile(t, dir, "a.go", nLines(5))
	writeFile(t, dir, "pawl.yaml", `dimensions:
  - id: "tests"
    title: "Failures"
    direction: "lower-is-better"
    builtin: "junit"
    options:
      file: "junit.xml"
  - id: "long-files"
    title: "Long files"
    direction: "lower-is-better"
    builtin: "file-length"
    options:
      include: ["*.go"]
      threshold: 100
`)
	setMtime(t, dir, "junit.xml", time.Now().Add(-25*time.Hour))

	res := runPawl(t, dir, baseEnv(), "record", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	m := mustMetric(t, parseReport(t, res.stdout), "tests")
	if m.Artifact == nil || m.Artifact.Path != "junit.xml" {
		t.Fatalf("record verdict lost the artifact: %+v", m.Artifact)
	}
	if m.Artifact.AgeSeconds < 24*3600 {
		t.Errorf("artifact.age_seconds = %d, want ≥ a day", m.Artifact.AgeSeconds)
	}

	// record --only tests: "tests" is measured (artifact), "long-files" is
	// copied from the snapshot (no artifact — nothing was read for it).
	res = runPawl(t, dir, baseEnv(), "record", "--only", "tests", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("record --only exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	rep := parseReport(t, res.stdout)
	if m := mustMetric(t, rep, "tests"); m.Artifact == nil {
		t.Errorf("measured dimension lost its artifact under record --only")
	}
	preserved := mustMetric(t, rep, "long-files")
	if preserved.MeasurementState != "preserved" {
		t.Fatalf("long-files measurement_state = %q, want preserved", preserved.MeasurementState)
	}
	if preserved.Artifact != nil {
		t.Errorf("a preserved dimension reports artifact %+v — this run measured nothing for it", preserved.Artifact)
	}
}

// A measurement that reads no file has no artifact to describe. The field must
// be absent rather than a zero-valued object claiming an unnamed file of age 0.
func TestNoArtifactFieldForMeasurementsThatReadNoFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "src.xml", junitMixedFixture)
	writeFile(t, dir, "a.go", nLines(5))
	writeFile(t, dir, "pawl.yaml", `dimensions:
  - id: "stdout-junit"
    title: "Failures via stdout"
    direction: "lower-is-better"
    builtin: "junit"
    options:
      command: 'cat "$PAWL_ROOT/src.xml"'
  - id: "long-files"
    title: "Long files"
    direction: "lower-is-better"
    builtin: "file-length"
    options:
      include: ["*.go"]
      threshold: 100
`)

	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("check exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	rep := parseReport(t, res.stdout)
	for _, id := range []string{"stdout-junit", "long-files"} {
		if m := mustMetric(t, rep, id); m.Artifact != nil {
			t.Errorf("metric %q reads no file but reports artifact %+v", id, m.Artifact)
		}
	}
	if strings.Contains(res.stdout, `"artifact"`) {
		t.Errorf("no dimension read a file, so no artifact key belongs in the output:\n%s", res.stdout)
	}
}

// The snapshot is the comparison baseline, not a log of how it was produced.
// Artifact metadata changes on every run without the measurement changing, so
// writing it into the snapshot would churn a file whose whole purpose is to
// only move when a number moves.
func TestArtifactMetadataStaysOutOfTheSnapshot(t *testing.T) {
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
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	raw := readFile(t, filepath.Join(dir, "pawl.snapshot.json"))
	for _, key := range []string{"artifact", "age_seconds", "modified", "junit.xml"} {
		if strings.Contains(raw, key) {
			t.Errorf("snapshot carries %q — artifact metadata must stay in the verdict:\n%s", key, raw)
		}
	}
}

// Text mode is the human default and its stdout is a stable table. The
// freshness note goes to stderr next to the measurement progress lines, and
// only when it says something: an artifact this run generated is new by
// construction, so noting its age would be noise.
func TestTextModeNotesArtifactAgeOnStderrOnlyWhenNotGenerated(t *testing.T) {
	t.Run("file only", func(t *testing.T) {
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
		setMtime(t, dir, "junit.xml", time.Now().Add(-72*time.Hour))
		if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
			t.Fatalf("record exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
		}
		res := runPawl(t, dir, baseEnv(), "check")
		if res.exit != 0 {
			t.Fatalf("check exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
		if !strings.Contains(res.stderr, "junit.xml") {
			t.Errorf("stderr should name the artifact it read:\n%s", res.stderr)
		}
		if !strings.Contains(res.stderr, "3d") {
			t.Errorf("stderr should carry the artifact's age (3d):\n%s", res.stderr)
		}
		if strings.Contains(res.stdout, "junit.xml") {
			t.Errorf("stdout is the human table and must not gain the note:\n%s", res.stdout)
		}
	})

	t.Run("generated by command", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "src.xml", junitMixedFixture)
		writeFile(t, dir, "pawl.yaml", `dimensions:
  - id: "tests"
    title: "Failures"
    direction: "lower-is-better"
    builtin: "junit"
    options:
      command: 'cp "$PAWL_ROOT/src.xml" "$PAWL_ROOT/out.xml"'
      file: "out.xml"
`)
		if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
			t.Fatalf("record exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
		}
		res := runPawl(t, dir, baseEnv(), "check")
		if res.exit != 0 {
			t.Fatalf("check exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
		if strings.Contains(res.stderr, "out.xml") {
			t.Errorf("an artifact this run generated needs no freshness note:\n%s", res.stderr)
		}
	})
}
