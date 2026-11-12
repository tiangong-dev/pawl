package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// htmlSensitiveConfig counts lines in the .txt files of its directory. Its unit
// ("files > 1 lines") and any path containing `&` or `<` are exactly the
// strings encoding/json escapes to \u003e / \u0026 / \u003c by default.
func htmlSensitiveConfig() string {
	return buildConfig("", dimDef{
		id: "length", direction: "lower-is-better", builtin: "file-length",
		optionLines: []string{`threshold = 1`, `include = ["*.txt"]`},
	})
}

func htmlSensitiveFixture(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, dir, "big&file.txt", nLines(3))
	writeFile(t, dir, "other<file.txt", nLines(3))
	writeFile(t, dir, "pawl.yaml", htmlSensitiveConfig())
}

// A snapshot whose units and breakdown keys contain `<`, `>` or `&` is written
// verbatim, so it stays readable and its diffs reviewable. The document remains
// valid JSON and keeps the same measurement when read back.
func TestRecordWritesHTMLSensitiveCharactersVerbatim(t *testing.T) {
	dir := t.TempDir()
	htmlSensitiveFixture(t, dir)
	mustRecord(t, dir, htmlSensitiveConfig())

	snapPath := filepath.Join(dir, "pawl.snapshot.json")
	body := readFile(t, snapPath)
	for _, escape := range []string{`\u003c`, `\u003e`, `\u0026`} {
		if strings.Contains(body, escape) {
			t.Errorf("snapshot contains the %s escape:\n%s", escape, body)
		}
	}
	for _, want := range []string{`"files > 1 lines"`, `"big&file.txt"`, `"other<file.txt"`} {
		if !strings.Contains(body, want) {
			t.Errorf("snapshot is missing literal %s:\n%s", want, body)
		}
	}

	snap := readSnapshot(t, snapPath)
	metric := snap.Metrics["length"]
	if metric.Unit != "files > 1 lines" || metric.Value != 2 {
		t.Errorf("metric = %+v, want unit %q and value 2", metric, "files > 1 lines")
	}
	if metric.Breakdown["big&file.txt"] != 3 || metric.Breakdown["other<file.txt"] != 3 {
		t.Errorf("breakdown keys did not round trip: %+v", metric.Breakdown)
	}

	if _, err := exec.LookPath("jq"); err == nil {
		out, err := exec.Command("jq", "-e", ".metrics.length.unit", snapPath).CombinedOutput()
		if err != nil {
			t.Fatalf("jq could not parse the snapshot: %v\n%s", err, out)
		}
		if got := strings.TrimSpace(string(out)); got != `"files > 1 lines"` {
			t.Errorf("jq read unit %s, want %q", got, "files > 1 lines")
		}
	}

	if res := runPawl(t, dir, baseEnv(), "check"); res.exit != 0 {
		t.Fatalf("check of a verbatim snapshot exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// Snapshots recorded before HTML-sensitive characters stopped being escaped
// stay comparable: both spellings parse to the same strings, so check gives the
// same verdict for either, and it never rewrites the baseline.
func TestCheckReadsEscapedSnapshotLikeLiteralOne(t *testing.T) {
	literalDir := t.TempDir()
	htmlSensitiveFixture(t, literalDir)
	mustRecord(t, literalDir, htmlSensitiveConfig())

	literalBody := readFile(t, filepath.Join(literalDir, "pawl.snapshot.json"))
	escapedBody := strings.NewReplacer(">", `\u003e`, "&", `\u0026`, "<", `\u003c`).Replace(literalBody)
	if escapedBody == literalBody {
		t.Fatalf("fixture snapshot has no HTML-sensitive characters to escape:\n%s", literalBody)
	}

	legacyDir := t.TempDir()
	htmlSensitiveFixture(t, legacyDir)
	writeFile(t, legacyDir, "pawl.snapshot.json", escapedBody)

	literal := runPawl(t, literalDir, baseEnv(), "check", "--format", "json")
	legacy := runPawl(t, legacyDir, baseEnv(), "check", "--format", "json")
	if literal.exit != 0 || legacy.exit != 0 {
		t.Fatalf("check exits = %d (literal), %d (escaped), want 0 for both\nliteral stdout=%s\nescaped stdout=%s",
			literal.exit, legacy.exit, literal.stdout, legacy.stdout)
	}

	literalMetric, ok := metricByID(parseReport(t, literal.stdout), "length")
	if !ok || literalMetric.Unit != "files > 1 lines" {
		t.Fatalf("literal verdict metric = %+v, want unit %q", literalMetric, "files > 1 lines")
	}
	legacyMetric, ok := metricByID(parseReport(t, legacy.stdout), "length")
	if !ok {
		t.Fatal("escaped verdict has no length metric")
	}
	if legacyMetric.Unit != literalMetric.Unit {
		t.Errorf("units differ: literal %q, escaped %q", literalMetric.Unit, legacyMetric.Unit)
	}
	if legacyMetric.Current == nil || literalMetric.Current == nil || *legacyMetric.Current != *literalMetric.Current {
		t.Errorf("current values differ: literal %v, escaped %v", literalMetric.Current, legacyMetric.Current)
	}
	if got := readFile(t, filepath.Join(legacyDir, "pawl.snapshot.json")); got != escapedBody {
		t.Errorf("check rewrote the baseline:\nbefore\n%s\nafter\n%s", escapedBody, got)
	}
}
