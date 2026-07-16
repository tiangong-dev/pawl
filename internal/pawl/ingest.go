package pawl

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Report-format ingest builtins: sarif, junit, coverage. Each reads one of the
// ecosystem's standard machine report formats so a tool that already emits it
// becomes a pawl dimension with no wrapper — pawl sits on top of the tools it
// trusts rather than reimplementing them. See SPEC.md § Report-format ingest.

const (
	builtinSarif    = "sarif"
	builtinJUnit    = "junit"
	builtinCoverage = "coverage"
)

// readIngestReport fetches the report bytes from the shared command/file source
// WITHOUT gating on the command's exit code. SARIF/JUnit/coverage producers
// conventionally exit non-zero to signal findings/failures, so the honesty
// guard is a parseable report (the caller's job), not exit 0 — but a command
// that could not run at all (timeout, spawn failure) or a `file` that never
// materialized is still a measurement failure, never a silent zero.
func readIngestReport(cfg *Config, dim Dimension, stderr io.Writer, command, fileRel string) ([]byte, error) {
	if fileRel != "" {
		filePath := filepath.Join(cfg.Dir, fileRel)
		if command != "" {
			// The command produces the file — a leftover from an earlier run
			// must never satisfy this measurement.
			if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("clearing stale %s: %v", fileRel, err)
			}
			if _, _, err := runAdapterCommand(cfg, dim, stderr, command); err != nil {
				return nil, err
			}
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %v", fileRel, err)
		}
		return data, nil
	}
	stdout, _, err := runAdapterCommand(cfg, dim, stderr, command)
	if err != nil {
		return nil, err
	}
	return stdout, nil
}

func setOf(items []string) map[string]bool {
	if len(items) == 0 {
		return nil
	}
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

// --- SARIF ---------------------------------------------------------------

type sarifLog struct {
	// Pointer so a document that parses as JSON but lacks "runs" (wrong shape)
	// fails loud, while a legitimate empty log (`{"runs":[]}`) measures zero.
	Runs *[]sarifRun `json:"runs"`
}

type sarifRun struct {
	Results []sarifResult `json:"results"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Locations []sarifLocation `json:"locations"`
}

type sarifLocation struct {
	PhysicalLocation struct {
		ArtifactLocation struct {
			URI string `json:"uri"`
		} `json:"artifactLocation"`
		Region struct {
			StartLine int `json:"startLine"`
		} `json:"region"`
	} `json:"physicalLocation"`
}

// measureSarif counts the results in a SARIF log (CodeQL, Semgrep, and many
// linters emit it), optionally filtered by ruleId and level, and builds a
// per-file:line breakdown from each result's first physical location.
func measureSarif(cfg *Config, dim Dimension, stderr io.Writer) (MeasureResult, error) {
	command, _ := dim.Options["command"].(string)
	fileRel, _ := dim.Options["file"].(string)
	data, err := readIngestReport(cfg, dim, stderr, command, fileRel)
	if err != nil {
		return MeasureResult{}, err
	}

	var log sarifLog
	if err := json.Unmarshal(data, &log); err != nil {
		return MeasureResult{}, fmt.Errorf("not SARIF JSON: %v (%.200s)", err, data)
	}
	if log.Runs == nil {
		return MeasureResult{}, fmt.Errorf("not a SARIF log: missing \"runs\" array (%.200s)", data)
	}

	ruleFilter := setOf(stringList(dim.Options["rules"]))
	levelFilter := setOf(stringList(dim.Options["levels"]))

	findings := newFileFindings(cfg)
	count := 0.0
	for _, run := range *log.Runs {
		for _, r := range run.Results {
			if ruleFilter != nil && !ruleFilter[r.RuleID] {
				continue
			}
			level := r.Level
			if level == "" {
				level = "warning" // the SARIF default when a result omits level
			}
			if levelFilter != nil && !levelFilter[level] {
				continue
			}
			count++
			if uri, line, ok := sarifResultLocation(r); ok {
				findings.add(uri, line, 1)
			}
		}
	}
	// value includes results with no location; breakdown carries only the
	// located ones (an unlocatable finding has nothing to attribute to a file).
	res := findings.result("findings")
	res.Value = count
	return res, nil
}

func sarifResultLocation(r sarifResult) (uri string, line int, ok bool) {
	if len(r.Locations) == 0 {
		return "", 0, false
	}
	phys := r.Locations[0].PhysicalLocation
	uri = phys.ArtifactLocation.URI
	if uri == "" {
		return "", 0, false
	}
	uri = strings.TrimPrefix(uri, "file://")
	return uri, phys.Region.StartLine, true
}

// --- JUnit ---------------------------------------------------------------

type junitRoot struct {
	XMLName   xml.Name
	Suites    []junitTestSuite `xml:"testsuite"`
	TestCases []junitTestCase  `xml:"testcase"`
}

type junitTestSuite struct {
	Suites    []junitTestSuite `xml:"testsuite"`
	TestCases []junitTestCase  `xml:"testcase"`
}

type junitTestCase struct {
	Failures []struct{} `xml:"failure"`
	Errors   []struct{} `xml:"error"`
	Skipped  []struct{} `xml:"skipped"`
}

// measureJUnit reads a JUnit XML report and counts one quantity derived from
// the <testcase> elements themselves (not the suite-level attributes, which
// producers compute inconsistently — the testcases are the single truth).
func measureJUnit(cfg *Config, dim Dimension, stderr io.Writer) (MeasureResult, error) {
	command, _ := dim.Options["command"].(string)
	fileRel, _ := dim.Options["file"].(string)
	countKind := "failures"
	if c, ok := dim.Options["count"].(string); ok && c != "" {
		countKind = c
	}

	data, err := readIngestReport(cfg, dim, stderr, command, fileRel)
	if err != nil {
		return MeasureResult{}, err
	}

	var root junitRoot
	if err := xml.Unmarshal(data, &root); err != nil {
		return MeasureResult{}, fmt.Errorf("not JUnit XML: %v (%.200s)", err, data)
	}
	// A document that parses as XML but is not rooted at <testsuites>/<testsuite>
	// is the wrong shape — some other XML that merely contains a <testcase> must
	// not read as a test result.
	if root.XMLName.Local != "testsuites" && root.XMLName.Local != "testsuite" {
		return MeasureResult{}, fmt.Errorf("not a JUnit report: root element is <%s>, want <testsuites> or <testsuite>", root.XMLName.Local)
	}
	cases := collectTestCases(root.Suites, root.TestCases)
	if len(cases) == 0 {
		return MeasureResult{}, fmt.Errorf("JUnit report has no <testcase> elements")
	}

	tests := len(cases)
	failures, skipped := 0, 0
	for _, c := range cases {
		// Detect each state independently so `skipped` really is "testcases with
		// a <skipped> child" (per the contract). A testcase that is both failed
		// and skipped is contradictory — fail loud rather than pick one silently.
		hasFail := len(c.Failures) > 0 || len(c.Errors) > 0
		hasSkip := len(c.Skipped) > 0
		if hasFail && hasSkip {
			return MeasureResult{}, fmt.Errorf("contradictory <testcase>: both failed/errored and skipped")
		}
		if hasFail {
			failures++
		}
		if hasSkip {
			skipped++
		}
	}

	var value float64
	switch countKind {
	case "failures":
		value = float64(failures)
	case "tests":
		value = float64(tests)
	case "skipped":
		value = float64(skipped)
	case "passing":
		value = float64(tests - failures - skipped)
	default:
		return MeasureResult{}, fmt.Errorf("unknown count %q", countKind)
	}
	return MeasureResult{Value: value, Unit: countKind}, nil
}

func collectTestCases(suites []junitTestSuite, direct []junitTestCase) []junitTestCase {
	all := append([]junitTestCase{}, direct...)
	for _, s := range suites {
		all = append(all, s.TestCases...)
		all = append(all, collectTestCases(s.Suites, nil)...)
	}
	return all
}

// --- Coverage ------------------------------------------------------------

// measureCoverage reads a coverage report (lcov's text, cobertura's XML) and
// computes a coverage percentage for the requested metric — the two formats
// json-value cannot read. A report with zero of the requested unit found is a
// measurement failure, never a silent 0 or 100.
func measureCoverage(cfg *Config, dim Dimension, stderr io.Writer) (MeasureResult, error) {
	fileRel, _ := dim.Options["file"].(string)
	format, _ := dim.Options["format"].(string)
	command, _ := dim.Options["command"].(string)
	metric := "lines"
	if m, ok := dim.Options["metric"].(string); ok && m != "" {
		metric = m
	}

	data, err := readIngestReport(cfg, dim, stderr, command, fileRel)
	if err != nil {
		return MeasureResult{}, err
	}

	var pct float64
	switch format {
	case "lcov":
		pct, err = lcovPercent(data, metric)
	case "cobertura":
		pct, err = coberturaPercent(data, metric)
	default:
		return MeasureResult{}, fmt.Errorf("unknown coverage format %q", format)
	}
	if err != nil {
		return MeasureResult{}, err
	}
	return MeasureResult{Value: pct, Unit: "%"}, nil
}

// lcovPercent sums the found/hit counters for the metric across every record in
// an lcov .info file. LF/LH = lines, FNF/FNH = functions, BRF/BRH = branches.
func lcovPercent(data []byte, metric string) (float64, error) {
	var foundKey, hitKey string
	switch metric {
	case "lines":
		foundKey, hitKey = "LF", "LH"
	case "functions":
		foundKey, hitKey = "FNF", "FNH"
	case "branches":
		foundKey, hitKey = "BRF", "BRH"
	default:
		return 0, fmt.Errorf("unknown metric %q", metric)
	}

	// Counters must be non-negative finite numbers — ParseFloat happily accepts
	// "NaN", "Inf", and negatives, any of which would otherwise yield a bogus
	// percentage (e.g. LF:-1 LH:-1 → 100%) instead of a measurement failure.
	readCounter := func(key, raw string) (float64, error) {
		n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return 0, fmt.Errorf("lcov %s record is not a number: %q", key, raw)
		}
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return 0, fmt.Errorf("lcov %s record is not a non-negative finite number: %q", key, raw)
		}
		return n, nil
	}

	// Well-formed lcov emits the found/hit summary counters in pairs inside
	// each SF:…end_of_record record — never outside one. Both structure rules
	// are enforced: a counter outside any record is malformed even as a
	// balanced pair (stray counters would silently skew the total), and
	// within a record the selected counters must pair (a merely
	// globally-balanced report — LF in one record, LH in another — is a
	// truncated or mangled report whose surviving counters would fabricate a
	// percentage). A record with neither counter of the selected metric is
	// fine.
	var found, hit float64
	inRecord := false
	recFound, recHit := 0, 0
	flushRecord := func() error {
		if recFound != recHit {
			return fmt.Errorf("lcov report is malformed: a record has %d %s counter(s) but %d %s counter(s) — they must pair within the same record",
				recFound, foundKey, recHit, hitKey)
		}
		recFound, recHit = 0, 0
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "SF:"):
			// A missing end_of_record before the next SF must not let an
			// unpaired counter escape validation.
			if err := flushRecord(); err != nil {
				return 0, err
			}
			inRecord = true
		case line == "end_of_record":
			if err := flushRecord(); err != nil {
				return 0, err
			}
			inRecord = false
		default:
			if v, ok := strings.CutPrefix(line, foundKey+":"); ok {
				if !inRecord {
					return 0, fmt.Errorf("lcov report is malformed: %s counter outside any SF record", foundKey)
				}
				n, err := readCounter(foundKey, v)
				if err != nil {
					return 0, err
				}
				found += n
				recFound++
			} else if v, ok := strings.CutPrefix(line, hitKey+":"); ok {
				if !inRecord {
					return 0, fmt.Errorf("lcov report is malformed: %s counter outside any SF record", hitKey)
				}
				n, err := readCounter(hitKey, v)
				if err != nil {
					return 0, err
				}
				hit += n
				recHit++
			}
		}
	}
	// A truncated trailing record (EOF without end_of_record) validates too.
	if err := flushRecord(); err != nil {
		return 0, err
	}
	if found == 0 {
		return 0, fmt.Errorf("no %s coverage data in lcov report", metric)
	}
	if hit > found {
		return 0, fmt.Errorf("lcov report has more %s hit (%v) than found (%v)", metric, hit, found)
	}
	return hit / found * 100, nil
}

// coberturaPercent reads the root <coverage> element's line-rate / branch-rate
// (a 0–1 fraction) and scales it to a percentage. cobertura carries no
// functions rate, so metric "functions" never reaches here (config rejects it).
func coberturaPercent(data []byte, metric string) (float64, error) {
	var cov struct {
		XMLName    xml.Name
		LineRate   *float64 `xml:"line-rate,attr"`
		BranchRate *float64 `xml:"branch-rate,attr"`
	}
	if err := xml.Unmarshal(data, &cov); err != nil {
		return 0, fmt.Errorf("not cobertura XML: %v", err)
	}
	// The rate attributes exist on the root <coverage> element; some other XML
	// that merely carries a line-rate attribute must not read as coverage.
	if cov.XMLName.Local != "coverage" {
		return 0, fmt.Errorf("not a cobertura report: root element is <%s>, want <coverage>", cov.XMLName.Local)
	}
	var rate *float64
	var attr string
	switch metric {
	case "lines":
		rate, attr = cov.LineRate, "line-rate"
	case "branches":
		rate, attr = cov.BranchRate, "branch-rate"
	default:
		return 0, fmt.Errorf("unknown metric %q", metric)
	}
	if rate == nil {
		return 0, fmt.Errorf("no %s coverage data: cobertura report has no %s attribute", metric, attr)
	}
	if math.IsNaN(*rate) || math.IsInf(*rate, 0) || *rate < 0 || *rate > 1 {
		return 0, fmt.Errorf("cobertura %s is not a fraction in [0,1]: %v", attr, *rate)
	}
	return *rate * 100, nil
}
