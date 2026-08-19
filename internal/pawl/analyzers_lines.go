package pawl

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// The line-oriented analyzer: one regex turns any tool's `path:line: message`
// output into the same findings a SARIF document produces, so a tool pawl has
// never heard of gets the capability the named builtins have — one run feeding
// several dimensions, each selecting its own rules or levels.
//
// It deliberately knows nothing about any particular tool. What it cannot do,
// it refuses rather than approximates: there is no rule catalog in line output,
// so `verify_rules` has no counterpart here, and the paths it sees are the
// files that *had* findings, not the files that were scanned, so `min_files`
// would be a lie and is rejected at config load.
const builtinLines = "lines"

// decodeLineReport applies the analyzer's regex to each non-empty line. Every
// such line must match: an unmatched line is a measurement failure, the same
// honesty rule `extract: regex` follows. It is also the closest thing a
// line-oriented tool has to rule verification — a pattern that stopped matching
// because the tool changed its output format fails loudly instead of reporting
// that every dimension sourcing it dropped to zero.
func decodeLineReport(data []byte, re *regexp.Regexp) (analyzerReport, error) {
	pathIdx, lineIdx, ruleIdx, levelIdx := -1, -1, -1, -1
	for i, name := range re.SubexpNames() {
		switch name {
		case "path":
			pathIdx = i
		case "line":
			lineIdx = i
		case "rule":
			ruleIdx = i
		case "level":
			levelIdx = i
		}
	}
	report := analyzerReport{}
	for _, raw := range strings.Split(string(data), "\n") {
		text := strings.TrimSuffix(raw, "\r")
		if strings.TrimSpace(text) == "" {
			continue
		}
		m := re.FindStringSubmatch(text)
		if m == nil {
			return analyzerReport{}, fmt.Errorf(
				"regex did not match line %q — filter the tool's summary lines out of the command, or fix the pattern", text)
		}
		finding := analyzerFinding{}
		if ruleIdx >= 0 {
			finding.RuleID = m[ruleIdx]
		}
		if levelIdx >= 0 {
			finding.Level = m[levelIdx]
		}
		if pathIdx >= 0 {
			path := m[pathIdx]
			finding.Path = &path
			if lineIdx >= 0 {
				if n, err := strconv.Atoi(m[lineIdx]); err == nil && n > 0 {
					finding.Line = &n
				}
			}
		}
		report.Findings = append(report.Findings, finding)
	}
	return report, nil
}

func runLinesAnalyzer(cfg *Config, dim Dimension, analyzer Analyzer, stderr io.Writer) (analyzerReport, error) {
	command, _ := analyzer.Options["command"].(string)
	valid, _ := exitCodeSet(analyzer.Options["valid_exit_codes"])
	stdout, exitCode, err := runAdapterCommand(cfg, dim, stderr, command)
	if err != nil {
		return analyzerReport{}, err
	}
	if !exitAccepted(valid, exitCode) {
		return analyzerReport{}, exitCodeError(valid, exitCode)
	}
	return decodeLineReport(stdout, analyzer.Regex)
}
