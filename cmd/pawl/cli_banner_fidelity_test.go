package main

// assets/banner.svg is the first thing a reader sees, and it claims to be a
// `pawl check -q` run rather than an impression of one. Nothing enforced
// that claim, so three separate drafts shipped output the CLI has never
// printed — a `measuring 4 dimensions…` progress line, coloured dots where
// the CLI emits emoji, and one table's column grid laid under another
// table's data. Each was caught by hand, after the fact, by someone
// happening to run the command.
//
// This rebuilds the banner's fixture, runs the real binary against it, and
// reconstructs the terminal text back out of the SVG by mapping each
// <text> element's x coordinate onto the character grid it is drawn on.
// Comparing the two catches a changed table format, a changed column
// width, and a mistyped coordinate with the same assertion.

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The banner draws 17px monospace at a 0.6em advance — 10.2px per character —
// with the terminal's text column starting at x=473. Both numbers are stated in
// the SVG's own comment; if either changes there without changing here, every
// reconstructed line shifts and this test fails loudly rather than silently
// approving a new grid.
const (
	bannerOriginX = 473.0
	bannerAdvance = 10.2
)

// pawl gates its own Go sources with a `todo-markers` dimension that scans
// **/*.go for exactly these two words. Spelling either of them out here would
// make this file a regression in the gate it exists to document — so the
// fixture assembles them instead. Loosening the dimension's exclude list to
// admit this file would have been the other way out, and the wrong one.
var (
	todoMarker  = "TO" + "DO"
	fixmeMarker = "FIX" + "ME"
)

// bannerFixture writes the four-dimension repo the banner depicts. The numbers
// are chosen so the run produces one of each status the table can show:
// eslint-warnings improves, todo-markers and line-coverage hold, complexity
// regresses and is attributable to a single file.
func bannerFixture(t *testing.T, dir string) {
	t.Helper()
	markers := strings.NewReplacer("@MARK_A@", todoMarker, "@MARK_B@", fixmeMarker)
	writeFile(t, dir, "pawl.yaml", markers.Replace(`snapshot: "pawl.snapshot.json"

dimensions:
  - id: "eslint-warnings"
    title: "eslint warnings"
    direction: "lower-is-better"
    gate: "total"
    command: "cat eslint-report.txt"
    extract:
      regex: '^(?P<path>[^:]+):(?P<line>\d+):'

  - id: "todo-markers"
    title: "@MARK_A@ / @MARK_B@ markers"
    direction: "lower-is-better"
    gate: "per-file-count"
    builtin: "pattern-count"
    options:
      pattern: '@MARK_A@|@MARK_B@'
      include: ["**/*.ts"]

  - id: "line-coverage"
    title: "line coverage %"
    direction: "higher-is-better"
    gate: "total"
    command: "cat coverage.txt"
    extract: number

  - id: "complexity"
    title: "cyclomatic complexity per function"
    direction: "lower-is-better"
    gate: "per-file-count"
    builtin: "pattern-count"
    options:
      pattern: 'BRANCH'
      include: ["**/*.ts"]
`))
	writeFile(t, dir, "coverage.txt", "71.2\n")
	writeFile(t, dir, "src/render.ts", strings.Repeat("  BRANCH // render\n", 6))
	writeFile(t, dir, "src/notes.ts", strings.Repeat("// "+todoMarker+": later\n", 12))
}

func bannerLintReport(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "src/mod%d.ts:%d: warning  unused var\n", i%37, i)
	}
	return b.String()
}

// svgText is one <text> or <tspan>, flattened enough to place on the grid.
type svgText struct {
	Class    string    `xml:"class,attr"`
	X        *float64  `xml:"x,attr"`
	Y        *float64  `xml:"y,attr"`
	Anchor   string    `xml:"text-anchor,attr"`
	Chardata string    `xml:",chardata"`
	Tspans   []svgText `xml:"tspan"`
}

func (s svgText) flat() string {
	out := s.Chardata
	for _, sp := range s.Tspans {
		out += sp.Chardata
	}
	return out
}

// bannerLines reconstructs the terminal content of the SVG: every mono <text>
// grouped by baseline y, each string placed at the character column its x
// coordinate encodes. The agent's two spoken turns are excluded — they are the
// only text in the frame that is not CLI output, which is exactly why they
// carry their own classes.
func bannerLines(t *testing.T, svgPath string) []string {
	t.Helper()
	raw, err := os.ReadFile(svgPath)
	if err != nil {
		t.Fatalf("read banner: %v", err)
	}

	var doc struct {
		Texts  []svgText `xml:"text"`
		Groups []struct {
			Texts []svgText `xml:"text"`
		} `xml:"g"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse banner as XML: %v", err)
	}
	all := doc.Texts
	for _, g := range doc.Groups {
		all = append(all, g.Texts...)
	}

	rows := map[float64][]svgText{}
	var order []float64
	for _, el := range all {
		if el.X == nil || el.Y == nil || !strings.Contains(el.Class, "mono") {
			continue
		}
		// The agent speaking, and the shell prompt it precedes: neither is
		// `check` output, so neither belongs in the comparison.
		if strings.Contains(el.Class, "a1") || strings.Contains(el.Class, "a2") ||
			strings.Contains(el.Class, "prompt") {
			continue
		}
		if _, seen := rows[*el.Y]; !seen {
			order = append(order, *el.Y)
		}
		rows[*el.Y] = append(rows[*el.Y], el)
	}
	// Screen order is y order, not document order: the table's rows live inside
	// <g> wrappers that carry their reveal animation, so they are parsed after
	// the top-level lines that are drawn below them.
	sort.Float64s(order)

	var lines []string
	for _, y := range order {
		var buf []rune
		for _, el := range rows[y] {
			text := []rune(el.flat())
			col := int((*el.X-bannerOriginX)/bannerAdvance + 0.5)
			if el.Anchor == "end" {
				col -= len(text)
			}
			if col < 0 {
				t.Fatalf("banner: text %q at x=%v lands left of the text column", el.flat(), *el.X)
			}
			for len(buf) < col {
				buf = append(buf, ' ')
			}
			buf = append(buf[:col], append(text, buf[min(col+len(text), len(buf)):]...)...)
		}
		lines = append(lines, strings.TrimRight(string(buf), " "))
	}
	return lines
}

func nonBlank(lines []string) []string {
	out := []string{}
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestBannerDoesNotAdvertiseRetiredCommands(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "assets", "banner.svg"))
	if err != nil {
		t.Fatalf("read banner: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, "agent-md") {
		t.Fatal("README banner still advertises the retired `pawl agent-md` command")
	}
}

// TestBannerShowsWhatCheckPrints is the guard on the README's front door: the
// image claims to be captured output, so the output is captured again here and
// compared. A failure means either the banner drifted or `check`'s table did —
// both are worth stopping for, and the diff says which.
func TestBannerShowsWhatCheckPrints(t *testing.T) {
	dir := t.TempDir()
	bannerFixture(t, dir)

	// Baseline: 342 warnings, 12 markers, 71.2% coverage, complexity 15
	// (9 in parser.ts + 6 in render.ts).
	writeFile(t, dir, "eslint-report.txt", bannerLintReport(342))
	writeFile(t, dir, "src/parser.ts", strings.Repeat("  BRANCH // parser\n", 9))
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record: exit %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	// The change the banner depicts: four warnings fixed, three branches added
	// to one file.
	writeFile(t, dir, "eslint-report.txt", bannerLintReport(338))
	writeFile(t, dir, "src/parser.ts", strings.Repeat("  BRANCH // parser\n", 12))

	res := runPawl(t, dir, baseEnv(), "check", "-q")
	if res.exit != 1 {
		t.Fatalf("check -q: want exit 1 (regression), got %d\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}

	want := nonBlank(strings.Split(res.stdout, "\n"))
	got := nonBlank(bannerLines(t, filepath.Join("..", "..", "assets", "banner.svg")))

	if len(want) == 0 {
		t.Fatal("check -q printed nothing on a regression")
	}
	if len(got) != len(want) {
		t.Fatalf("banner has %d output lines, check printed %d\nbanner:\n%s\ncheck:\n%s",
			len(got), len(want), strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d differs\n banner: %q\n  check: %q", i+1, got[i], want[i])
		}
	}
}
