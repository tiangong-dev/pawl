package pawl

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type oxlintJSONReport struct {
	// Pointers distinguish a valid empty scan from a different JSON object
	// that happens to decode with Go zero values.
	Diagnostics   *[]oxlintDiagnostic `json:"diagnostics"`
	NumberOfFiles *int                `json:"number_of_files"`
}

type oxlintDiagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Filename string `json:"filename"`
	Labels   []struct {
		Span struct {
			Line int `json:"line"`
		} `json:"span"`
	} `json:"labels"`
}

func runOxlintAnalyzer(cfg *Config, dim Dimension, stderr io.Writer) (analyzerReport, error) {
	command, _ := dim.Options["command"].(string)
	stdout, exitCode, err := runAdapterCommand(cfg, dim, stderr, command)
	if err != nil {
		return analyzerReport{}, err
	}
	if exitCode != 0 && exitCode != 1 {
		return analyzerReport{}, fmt.Errorf("oxlint exited with fatal code %d", exitCode)
	}
	return decodeOxlintReport(stdout)
}

func decodeOxlintReport(data []byte) (analyzerReport, error) {
	var decoded oxlintJSONReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		return analyzerReport{}, fmt.Errorf("stdout is not Oxlint JSON: %v (stdout: %.200s)", err, data)
	}
	if decoded.Diagnostics == nil {
		return analyzerReport{}, fmt.Errorf("stdout is not Oxlint JSON: missing \"diagnostics\" array (stdout: %.200s)", data)
	}
	if decoded.NumberOfFiles == nil {
		return analyzerReport{}, fmt.Errorf("stdout is not Oxlint JSON: missing \"number_of_files\" (stdout: %.200s)", data)
	}
	if *decoded.NumberOfFiles < 0 {
		return analyzerReport{}, fmt.Errorf("stdout is not Oxlint JSON: number_of_files must be non-negative, got %d", *decoded.NumberOfFiles)
	}

	report := analyzerReport{FilesScanned: *decoded.NumberOfFiles}
	filesWithDiagnostics := map[string]bool{}
	for i, diagnostic := range *decoded.Diagnostics {
		if !validAnalyzerLevel(builtinOxlint, diagnostic.Severity) {
			return analyzerReport{}, fmt.Errorf("stdout is not Oxlint JSON: diagnostic %d has unsupported severity %q",
				i+1, diagnostic.Severity)
		}
		finding := analyzerFinding{RuleID: diagnostic.Code, Level: diagnostic.Severity}
		if diagnostic.Filename != "" {
			path := strings.TrimPrefix(diagnostic.Filename, "file://")
			filesWithDiagnostics[path] = true
			finding.Path = &path
			for _, label := range diagnostic.Labels {
				if label.Span.Line < 0 {
					return analyzerReport{}, fmt.Errorf("stdout is not Oxlint JSON: diagnostic %d has a negative label line", i+1)
				}
				if label.Span.Line > 0 {
					line := label.Span.Line
					finding.Line = &line
					break
				}
			}
		}
		report.Findings = append(report.Findings, finding)
	}
	if len(filesWithDiagnostics) > report.FilesScanned {
		return analyzerReport{}, fmt.Errorf(
			"stdout is not Oxlint JSON: diagnostics name %d file(s), but number_of_files is %d",
			len(filesWithDiagnostics), report.FilesScanned)
	}
	return report, nil
}

func verifyOxlintRules(cfg *Config, dim Dimension, commands, requiredRules []string, stderr io.Writer) error {
	if len(commands) == 0 {
		return nil
	}
	enabled := map[string]bool{}
	for _, command := range commands {
		stdout, exitCode, err := runAdapterCommand(cfg, dim, stderr, command)
		if err != nil {
			return fmt.Errorf("oxlint rule verification: %v", err)
		}
		if exitCode != 0 {
			return fmt.Errorf("oxlint rule verification command exited with code %d", exitCode)
		}
		var config struct {
			Rules map[string]any `json:"rules"`
		}
		if err := json.Unmarshal(stdout, &config); err != nil || config.Rules == nil {
			if err == nil {
				err = fmt.Errorf("missing rules object")
			}
			return fmt.Errorf("oxlint rule verification output is not --print-config JSON: %v", err)
		}
		for rule, severity := range config.Rules {
			if oxlintRuleSeverityEnabled(severity) {
				enabled[oxlintDiagnosticCode(rule)] = true
			}
		}
	}
	for _, rule := range requiredRules {
		if !enabled[rule] {
			return fmt.Errorf("configured rule %q is missing or disabled in every verified Oxlint config", rule)
		}
	}
	return nil
}

// Oxlint config keys use ESLint-style plugin/name spelling, while native JSON
// diagnostics use plugin(name). Core ESLint rules omit the plugin in config.
func oxlintDiagnosticCode(configRule string) string {
	if plugin, rule, ok := strings.Cut(configRule, "/"); ok {
		return plugin + "(" + rule + ")"
	}
	return "eslint(" + configRule + ")"
}

func oxlintRuleSeverityEnabled(value any) bool {
	if list, ok := value.([]any); ok {
		if len(list) == 0 {
			return false
		}
		value = list[0]
	}
	if severity, ok := value.(string); ok && severity == "allow" {
		return false
	}
	return lintRuleSeverityEnabled(value)
}
