package pawl

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// analyzerFinding is the normalized, lossless unit emitted by a named
// analyzer. Location absence is explicit; reducers decide how to project it
// into the legacy scalar + path:line snapshot shape.
type analyzerFinding struct {
	RuleID string
	Level  string
	Path   *string
	Line   *int
}

type analyzerReport struct {
	Findings           []analyzerFinding
	FilesScanned       int
	RuleCatalog        map[string]bool
	RuleCatalogPresent bool
	// Artifact is the file the analyzer read, if it read one. One analyzer run
	// feeds every dimension that sources it, so they all report the same
	// provenance — which is accurate: they all read that one file.
	Artifact *ArtifactInfo
}

type analyzerRun struct {
	once   sync.Once
	report analyzerReport
	err    error
}

type analyzerRuns map[string]*analyzerRun

func newAnalyzerRuns(cfg *Config) analyzerRuns {
	runs := analyzerRuns{}
	for id := range cfg.Analyzers {
		runs[id] = &analyzerRun{}
	}
	return runs
}

func (runs analyzerRuns) measure(cfg *Config, dim Dimension, stderr io.Writer) (MeasureResult, error) {
	analyzer := cfg.Analyzers[dim.Source]
	run := runs[dim.Source]
	run.once.Do(func() {
		required := requiredRulesForAnalyzer(cfg, dim.Source)
		run.report, run.err = executeAnalyzer(cfg, analyzer, required, stderr)
	})
	if run.err != nil {
		return MeasureResult{}, fmt.Errorf("analyzer %s: %v", dim.Source, run.err)
	}
	return reduceAnalyzerReport(cfg, analyzer, dim, run.report)
}

func requiredRulesForAnalyzer(cfg *Config, source string) []string {
	set := map[string]bool{}
	for _, dim := range cfg.Dimensions {
		if dim.Source != source {
			continue
		}
		rules, _ := strictStringList(dim.Options["rules"])
		for _, rule := range rules {
			set[rule] = true
		}
	}
	out := make([]string, 0, len(set))
	for rule := range set {
		out = append(out, rule)
	}
	sort.Strings(out)
	return out
}

func executeAnalyzer(cfg *Config, analyzer Analyzer, requiredRules []string, stderr io.Writer) (analyzerReport, error) {
	dim := Dimension{
		ID:      "analyzer:" + analyzer.ID,
		Timeout: analyzer.Timeout,
		Options: analyzer.Options,
		Builtin: analyzer.Builtin,
	}
	if analyzer.Builtin == builtinEslint {
		if err := verifyEslintRules(cfg, dim, analyzer.Verify, requiredRules, stderr); err != nil {
			return analyzerReport{}, err
		}
		command, _ := analyzer.Options["command"].(string)
		stdout, exitCode, err := runAdapterCommand(cfg, dim, stderr, command)
		if err != nil {
			return analyzerReport{}, err
		}
		if exitCode >= 2 {
			return analyzerReport{}, fmt.Errorf("eslint exited with fatal code %d", exitCode)
		}
		report, err := decodeEslintReport(stdout)
		if err != nil {
			return analyzerReport{}, err
		}
		return validateAnalyzerReport(analyzer, report)
	}
	if analyzer.Builtin == builtinOxlint {
		if err := verifyOxlintRules(cfg, dim, analyzer.Verify, requiredRules, stderr); err != nil {
			return analyzerReport{}, err
		}
		report, err := runOxlintAnalyzer(cfg, dim, stderr)
		if err != nil {
			return analyzerReport{}, err
		}
		return validateAnalyzerReport(analyzer, report)
	}

	data, artifact, err := readNamedSarifReport(cfg, dim, analyzer.Options, stderr)
	if err != nil {
		return analyzerReport{}, err
	}
	report, err := decodeSarifReport(data)
	if err != nil {
		return analyzerReport{}, err
	}
	report.Artifact = artifact
	if verify, _ := analyzer.Options["verify_rules"].(bool); verify {
		if len(requiredRules) == 0 {
			return analyzerReport{}, fmt.Errorf("verify_rules requires at least one referencing dimension with a rules selector")
		}
		if !report.RuleCatalogPresent {
			return analyzerReport{}, fmt.Errorf("SARIF rule verification requested, but the report has no tool.driver.rules catalog")
		}
		for _, rule := range requiredRules {
			if !report.RuleCatalog[rule] {
				return analyzerReport{}, fmt.Errorf("configured rule %q is missing from the SARIF rule catalog", rule)
			}
		}
	}
	return validateAnalyzerReport(analyzer, report)
}

// readNamedSarifReport acquires one fresh SARIF document. A named analyzer can
// declare valid_exit_codes when its producer distinguishes "findings found"
// from fatal errors; unlike the legacy generic SARIF builtin, that lets Pawl
// reject a parseable-but-fatal run without hard-coding one tool's conventions.
func readNamedSarifReport(cfg *Config, dim Dimension, options map[string]any, stderr io.Writer) ([]byte, *ArtifactInfo, error) {
	command, _ := options["command"].(string)
	fileRel, _ := options["file"].(string)
	validExitCodes, _ := exitCodeSet(options["valid_exit_codes"])

	run := func() ([]byte, error) {
		stdout, exitCode, err := runAdapterCommand(cfg, dim, stderr, command)
		if err != nil {
			return nil, err
		}
		if validExitCodes != nil && !validExitCodes[exitCode] {
			return nil, fmt.Errorf("command exited with code %d (valid_exit_codes: %s)",
				exitCode, formatExitCodes(validExitCodes))
		}
		return stdout, nil
	}

	if fileRel == "" {
		data, err := run()
		return data, nil, err
	}
	filePath := filepath.Join(cfg.Dir, fileRel)
	if command != "" {
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("clearing stale %s: %v", fileRel, err)
		}
		if _, err := run(); err != nil {
			return nil, nil, err
		}
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %v", fileRel, err)
	}
	return data, statArtifact(cfg, fileRel, command != ""), nil
}

func formatExitCodes(codes map[int]bool) string {
	ordered := make([]int, 0, len(codes))
	for code := range codes {
		ordered = append(ordered, code)
	}
	sort.Ints(ordered)
	parts := make([]string, len(ordered))
	for i, code := range ordered {
		parts[i] = fmt.Sprint(code)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func validateAnalyzerReport(analyzer Analyzer, report analyzerReport) (analyzerReport, error) {
	if raw, ok := numberOption(analyzer.Options, "min_files"); ok {
		if raw < 0 || raw != float64(int(raw)) {
			return analyzerReport{}, fmt.Errorf("min_files must be a non-negative integer")
		}
		if report.FilesScanned < int(raw) {
			return analyzerReport{}, fmt.Errorf("scanned %d file(s), expected at least %d", report.FilesScanned, int(raw))
		}
	}
	return report, nil
}

func decodeEslintReport(data []byte) (analyzerReport, error) {
	var results []eslintFileResult
	if err := json.Unmarshal(data, &results); err != nil {
		return analyzerReport{}, fmt.Errorf("stdout is not an ESLint JSON array: %v (stdout: %.200s)", err, data)
	}
	report := analyzerReport{FilesScanned: len(results)}
	for _, file := range results {
		path := file.FilePath
		for _, msg := range file.Messages {
			finding := analyzerFinding{RuleID: msg.RuleID, Path: &path}
			if msg.Line > 0 {
				line := msg.Line
				finding.Line = &line
			}
			report.Findings = append(report.Findings, finding)
		}
	}
	return report, nil
}

func decodeSarifReport(data []byte) (analyzerReport, error) {
	var log sarifLog
	if err := json.Unmarshal(data, &log); err != nil {
		return analyzerReport{}, fmt.Errorf("not SARIF JSON: %v (%.200s)", err, data)
	}
	if log.Runs == nil {
		return analyzerReport{}, fmt.Errorf("not a SARIF log: missing \"runs\" array (%.200s)", data)
	}
	report := analyzerReport{}
	for _, run := range *log.Runs {
		report.FilesScanned += len(run.Artifacts)
		if run.Tool.Driver.Rules != nil {
			report.RuleCatalogPresent = true
			if report.RuleCatalog == nil {
				report.RuleCatalog = map[string]bool{}
			}
			for _, rule := range *run.Tool.Driver.Rules {
				if rule.ID != "" {
					report.RuleCatalog[rule.ID] = true
				}
			}
		}
		for _, result := range run.Results {
			level := result.Level
			if level == "" {
				level = "warning"
			}
			finding := analyzerFinding{RuleID: result.RuleID, Level: level}
			if uri, line, ok := sarifResultLocation(result); ok {
				path := uri
				finding.Path = &path
				if line > 0 {
					finding.Line = &line
				}
			}
			report.Findings = append(report.Findings, finding)
		}
	}
	return report, nil
}

func reduceAnalyzerReport(cfg *Config, analyzer Analyzer, dim Dimension, report analyzerReport) (MeasureResult, error) {
	rules, _ := strictStringList(dim.Options["rules"])
	levels, _ := strictStringList(dim.Options["levels"])
	ruleFilter := setOf(rules)
	levelFilter := setOf(levels)
	findings := newFileFindings(cfg)
	total := 0.0
	for _, finding := range report.Findings {
		if ruleFilter != nil && !ruleFilter[finding.RuleID] {
			continue
		}
		if levelFilter != nil && !levelFilter[finding.Level] {
			continue
		}
		total++
		if finding.Path != nil {
			line := 0
			if finding.Line != nil {
				line = *finding.Line
			}
			findings.add(*finding.Path, line, 1)
		}
	}
	unit := "findings"
	if analyzer.Builtin == builtinEslint || analyzer.Builtin == builtinOxlint {
		unit = "issues"
	}
	result := findings.result(unit)
	result.Value = total
	result.Artifact = report.Artifact
	return result, nil
}

func verifyEslintRules(cfg *Config, dim Dimension, commands, requiredRules []string, stderr io.Writer) error {
	if len(commands) == 0 {
		return nil
	}
	enabled := map[string]bool{}
	for _, command := range commands {
		stdout, exitCode, err := runAdapterCommand(cfg, dim, stderr, command)
		if err != nil {
			return fmt.Errorf("eslint rule verification: %v", err)
		}
		if exitCode != 0 {
			return fmt.Errorf("eslint rule verification command exited with code %d", exitCode)
		}
		var config struct {
			Rules map[string]any `json:"rules"`
		}
		if err := json.Unmarshal(stdout, &config); err != nil || config.Rules == nil {
			if err == nil {
				err = fmt.Errorf("missing rules object")
			}
			return fmt.Errorf("eslint rule verification output is not --print-config JSON: %v", err)
		}
		for rule, severity := range config.Rules {
			if lintRuleSeverityEnabled(severity) {
				enabled[rule] = true
			}
		}
	}
	for _, rule := range requiredRules {
		if !enabled[rule] {
			return fmt.Errorf("configured rule %q is missing or disabled in every verified ESLint config", rule)
		}
	}
	return nil
}

func lintRuleSeverityEnabled(value any) bool {
	if list, ok := value.([]any); ok {
		if len(list) == 0 {
			return false
		}
		value = list[0]
	}
	switch v := value.(type) {
	case float64:
		return v != 0
	case string:
		return v != "" && v != "off" && v != "0"
	default:
		return false
	}
}
