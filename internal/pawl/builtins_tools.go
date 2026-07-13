package pawl

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Tool builtins wrap well-known analyzers' machine output so a project only
// supplies the tool invocation — pawl owns the format knowledge AND the
// tool's exit-code semantics, which the raw exec contract (exit 0 or bust)
// cannot express without `|| true` hacks that lose real-failure detection.

const (
	builtinEslint          = "eslint"
	builtinJscpd           = "jscpd"
	builtinSwiftComplexity = "swift-complexity"
	builtinJSONValue       = "json-value"
)

// measureJSONValue reads one finite number out of a tool's JSON — the generic
// reader behind coverage %, passing-test counts, and type-coverage, and the
// natural home for higher-is-better dimensions. The JSON comes from the
// command's stdout, or from a file (optionally produced by the command, with
// the jscpd stale-artifact guard applied).
func measureJSONValue(cfg *Config, dim Dimension, stderr io.Writer) (MeasureResult, error) {
	path, _ := dim.Options["path"].(string)
	command, _ := dim.Options["command"].(string)
	fileRel, _ := dim.Options["file"].(string)
	unit := "count"
	if u, ok := dim.Options["unit"].(string); ok && u != "" {
		unit = u
	}

	var data []byte
	if fileRel != "" {
		filePath := filepath.Join(cfg.Dir, fileRel)
		if command != "" {
			// The command produces the file — a leftover from an earlier run
			// must never satisfy this measurement.
			if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
				return MeasureResult{}, fmt.Errorf("clearing stale %s: %v", fileRel, err)
			}
			if _, exitCode, err := runAdapterCommand(cfg, dim, stderr, command); err != nil {
				return MeasureResult{}, err
			} else if exitCode != 0 {
				return MeasureResult{}, fmt.Errorf("command exited with code %d", exitCode)
			}
		}
		var err error
		if data, err = os.ReadFile(filePath); err != nil {
			return MeasureResult{}, fmt.Errorf("reading %s: %v", fileRel, err)
		}
	} else {
		stdout, exitCode, err := runAdapterCommand(cfg, dim, stderr, command)
		if err != nil {
			return MeasureResult{}, err
		}
		if exitCode != 0 {
			return MeasureResult{}, fmt.Errorf("command exited with code %d", exitCode)
		}
		data = stdout
	}

	value, err := jsonNumberAtPath(data, path)
	if err != nil {
		source := "stdout"
		if fileRel != "" {
			source = fileRel
		}
		return MeasureResult{}, fmt.Errorf("%s: %v", source, err)
	}
	return MeasureResult{Value: value, Unit: unit}, nil
}

type eslintFileResult struct {
	FilePath string `json:"filePath"`
	Messages []struct {
		RuleID string `json:"ruleId"`
		Line   int    `json:"line"`
	} `json:"messages"`
}

// measureEslint runs the configured ESLint invocation and counts (optionally
// rule-filtered) messages from its --format json output. ESLint's exit 1
// means "problems found" — a valid measurement; only exit 2+ (config error,
// crash) is a measurement failure.
func measureEslint(cfg *Config, dim Dimension, stderr io.Writer) (MeasureResult, error) {
	command, _ := dim.Options["command"].(string)
	stdout, exitCode, err := runAdapterCommand(cfg, dim, stderr, command)
	if err != nil {
		return MeasureResult{}, err
	}
	if exitCode >= 2 {
		return MeasureResult{}, fmt.Errorf("eslint exited with fatal code %d", exitCode)
	}

	var results []eslintFileResult
	if err := json.Unmarshal(stdout, &results); err != nil {
		return MeasureResult{}, fmt.Errorf("stdout is not an ESLint JSON array: %v (stdout: %.200s)", err, stdout)
	}

	ruleFilter := map[string]bool{}
	for _, r := range stringList(dim.Options["rules"]) {
		ruleFilter[r] = true
	}

	findings := newFileFindings(cfg)
	for _, file := range results {
		for _, msg := range file.Messages {
			if len(ruleFilter) > 0 && !ruleFilter[msg.RuleID] {
				continue
			}
			findings.add(file.FilePath, msg.Line, 1)
		}
	}
	return findings.result("issues"), nil
}

// measureJscpd runs the configured jscpd invocation and reads the JSON
// report it writes — a named convenience over the json-value builtin that
// bakes in jscpd's report path and the duplicatedLines metric. jscpd must
// exit 0 (pawl does not use jscpd's own --threshold gating); a missing or
// malformed report is a measurement failure.
func measureJscpd(cfg *Config, dim Dimension, stderr io.Writer) (MeasureResult, error) {
	command, _ := dim.Options["command"].(string)
	reportRel, _ := dim.Options["report"].(string)
	reportPath := filepath.Join(cfg.Dir, reportRel)
	// A leftover report from an earlier run must never satisfy this
	// measurement — only a report the command just wrote counts.
	if err := os.Remove(reportPath); err != nil && !os.IsNotExist(err) {
		return MeasureResult{}, fmt.Errorf("clearing stale jscpd report %s: %v", reportRel, err)
	}
	_, exitCode, err := runAdapterCommand(cfg, dim, stderr, command)
	if err != nil {
		return MeasureResult{}, err
	}
	if exitCode != 0 {
		return MeasureResult{}, fmt.Errorf("jscpd exited with code %d", exitCode)
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		return MeasureResult{}, fmt.Errorf("jscpd report %s: %v", reportRel, err)
	}
	lines, err := jsonNumberAtPath(data, "statistics.total.duplicatedLines")
	if err != nil {
		return MeasureResult{}, fmt.Errorf("jscpd report %s: %v", reportRel, err)
	}
	return MeasureResult{Value: lines, Unit: "duplicated lines"}, nil
}

type swiftComplexityReport struct {
	// Pointer so stdout that parses as JSON but lacks the "files" key (wrong
	// shape) fails loud, while a legitimate empty run (`{"files":[]}`) still
	// measures zero.
	Files *[]swiftComplexityFile `json:"files"`
}

type swiftComplexityFile struct {
	FilePath  string `json:"filePath"`
	Functions []struct {
		// Pointers so a function missing the selected metric fails loud
		// rather than counting as a silent, non-offending zero.
		Cognitive  *float64 `json:"cognitiveComplexity"`
		Cyclomatic *float64 `json:"cyclomaticComplexity"`
		Location   struct {
			Line int `json:"line"`
		} `json:"location"`
	} `json:"functions"`
}

// measureSwiftComplexity runs the configured swift-complexity invocation and
// counts functions whose selected metric (cognitive by default) is at or above
// the threshold. swift-complexity's exit 1 conflates "found violations" with
// "bad path", so — unlike eslint — pawl requires exit 0 and does the
// thresholding itself, keeping the gate single-sourced in pawl.yaml.
func measureSwiftComplexity(cfg *Config, dim Dimension, stderr io.Writer) (MeasureResult, error) {
	command, _ := dim.Options["command"].(string)
	metric := "cognitive"
	if m, ok := dim.Options["metric"].(string); ok && m != "" {
		metric = m
	}
	threshold, _ := numberOption(dim.Options, "threshold")

	stdout, exitCode, err := runAdapterCommand(cfg, dim, stderr, command)
	if err != nil {
		return MeasureResult{}, err
	}
	if exitCode != 0 {
		return MeasureResult{}, fmt.Errorf("swift-complexity exited with code %d (its exit 1 conflates findings with errors — the command must not pass its own --threshold)", exitCode)
	}

	var report swiftComplexityReport
	if err := json.Unmarshal(stdout, &report); err != nil {
		return MeasureResult{}, fmt.Errorf("stdout is not swift-complexity JSON: %v (stdout: %.200s)", err, stdout)
	}
	if report.Files == nil {
		return MeasureResult{}, fmt.Errorf("stdout is not swift-complexity JSON: missing \"files\" (stdout: %.200s)", stdout)
	}

	findings := newFileFindings(cfg)
	for _, file := range *report.Files {
		for _, fn := range file.Functions {
			value := fn.Cognitive
			if metric == "cyclomatic" {
				value = fn.Cyclomatic
			}
			if value == nil {
				return MeasureResult{}, fmt.Errorf("%s: a function is missing its %s complexity field", relativizeToConfigDir(cfg, file.FilePath), metric)
			}
			if *value >= threshold {
				findings.add(file.FilePath, fn.Location.Line, 1)
			}
		}
	}
	return findings.result("functions"), nil
}
