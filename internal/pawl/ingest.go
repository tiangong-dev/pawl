package pawl

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Report-format ingest builtins: sarif, junit, coverage. Each reads one of the
// ecosystem's standard machine report formats so a tool that already emits it
// becomes a pawl dimension with no wrapper — pawl sits on top of the tools it
// trusts rather than reimplementing them. See spec/adapters/ingest.md.

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
func readIngestReport(cfg *Config, dim Dimension, stderr io.Writer, command, fileRel string) ([]byte, *ArtifactInfo, error) {
	if fileRel != "" {
		filePath := filepath.Join(cfg.Dir, fileRel)
		if command != "" {
			// The command produces the file — a leftover from an earlier run
			// must never satisfy this measurement.
			if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
				return nil, nil, fmt.Errorf("clearing stale %s: %v", fileRel, err)
			}
			if _, _, err := runAdapterCommand(cfg, dim, stderr, command); err != nil {
				return nil, nil, err
			}
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			if command == "" && errors.Is(err, fs.ErrNotExist) {
				return nil, nil, fmt.Errorf(`reading %s: file does not exist — this dimension has no "command", so an earlier step must produce this file before pawl runs (or the "file" path is wrong)`, fileRel)
			}
			return nil, nil, fmt.Errorf("reading %s: %v", fileRel, err)
		}
		return data, statArtifact(cfg, fileRel, command != ""), nil
	}
	stdout, _, err := runAdapterCommand(cfg, dim, stderr, command)
	if err != nil {
		return nil, nil, err
	}
	return stdout, nil, nil
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
	Results   []sarifResult    `json:"results"`
	Artifacts []map[string]any `json:"artifacts"`
	Tool      struct {
		Driver struct {
			// Pointer distinguishes an omitted catalog from a legitimate empty
			// one. Named analyzers can opt into fail-closed selector validation.
			Rules *[]sarifRule `json:"rules"`
		} `json:"driver"`
	} `json:"tool"`
}

type sarifRule struct {
	ID string `json:"id"`
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
	data, artifact, err := readIngestReport(cfg, dim, stderr, command, fileRel)
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
	if minFiles, ok := numberOption(dim.Options, "min_files"); ok {
		artifacts := 0
		for _, run := range *log.Runs {
			artifacts += len(run.Artifacts)
		}
		if artifacts < int(minFiles) {
			return MeasureResult{}, fmt.Errorf("SARIF report has %d artifact(s), expected at least %d", artifacts, int(minFiles))
		}
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
	res.Artifact = artifact
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

	data, artifact, err := readIngestReport(cfg, dim, stderr, command, fileRel)
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
	return MeasureResult{Value: value, Unit: countKind, Artifact: artifact}, nil
}

func collectTestCases(suites []junitTestSuite, direct []junitTestCase) []junitTestCase {
	all := append([]junitTestCase{}, direct...)
	for _, s := range suites {
		all = append(all, s.TestCases...)
		all = append(all, collectTestCases(s.Suites, nil)...)
	}
	return all
}
