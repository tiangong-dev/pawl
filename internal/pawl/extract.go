package pawl

import (
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// The declarative extract layer. A command dimension may set `extract` to derive
// its measurement from the tool's raw output, so trivial metrics need no wrapper
// script that only reformats into the `{value,unit,breakdown}` JSON contract.
// See SPEC.md § Declarative extract layer.

const (
	extractNumber   = "number"
	extractLines    = "lines"
	extractRegex    = "regex"
	extractJSONPath = "json_path"
)

// ExtractSpec is a validated extract declaration. Form selects the derivation;
// the remaining fields carry the form-specific inputs.
type ExtractSpec struct {
	Form     string
	Regex    *regexp.Regexp
	JSONPath string
	Unit     string
}

// parseExtract validates the raw `extract:` YAML value (a string for the scalar
// forms, a map for the object forms) into an ExtractSpec. Every malformed shape
// is an error so the config aborts at load (exit 2) rather than half-run.
func parseExtract(v any) (*ExtractSpec, error) {
	switch e := v.(type) {
	case string:
		switch e {
		case extractNumber, extractLines:
			return &ExtractSpec{Form: e}, nil
		default:
			return nil, fmt.Errorf("unknown form %q — want %q, %q, or an object with %q/%q",
				e, extractNumber, extractLines, extractRegex, extractJSONPath)
		}
	case map[string]any:
		regexVal, hasRegex := e[extractRegex]
		jsonVal, hasJSON := e[extractJSONPath]
		unit, _ := e["unit"].(string)
		if hasRegex == hasJSON {
			return nil, fmt.Errorf("an object form needs exactly one of %q / %q", extractRegex, extractJSONPath)
		}
		if hasRegex {
			regexStr, ok := regexVal.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be a string", extractRegex)
			}
			re, err := regexp.Compile(regexStr)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", extractRegex, err)
			}
			return &ExtractSpec{Form: extractRegex, Regex: re, Unit: unit}, nil
		}
		jsonPath, ok := jsonVal.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be a string", extractJSONPath)
		}
		if jsonPath == "" {
			return nil, fmt.Errorf("%s must not be empty", extractJSONPath)
		}
		return &ExtractSpec{Form: extractJSONPath, JSONPath: jsonPath, Unit: unit}, nil
	default:
		return nil, fmt.Errorf("must be a string (%q/%q) or an object (%q/%q)",
			extractNumber, extractLines, extractRegex, extractJSONPath)
	}
}

// measureExtract runs the command and derives the measurement per the declared
// form. The command must exit 0; a non-zero exit, timeout, or output that cannot
// be extracted is a measurement failure (never a silent zero), matching the raw
// exec contract's honesty rule.
func measureExtract(cfg *Config, dim Dimension, stderr io.Writer) (MeasureResult, error) {
	stdout, exitCode, err := runAdapterCommand(cfg, dim, stderr, dim.Command)
	if err != nil {
		return MeasureResult{}, err
	}
	if exitCode != 0 {
		return MeasureResult{}, fmt.Errorf("command exited with an error: exit status %d", exitCode)
	}
	spec := dim.Extract
	switch spec.Form {
	case extractNumber:
		return extractNumberValue(stdout, spec)
	case extractLines:
		return extractLineCount(stdout, spec), nil
	case extractRegex:
		return extractByRegex(cfg, stdout, spec)
	case extractJSONPath:
		n, err := jsonNumberAtPath(stdout, spec.JSONPath)
		if err != nil {
			return MeasureResult{}, err
		}
		return MeasureResult{Value: n, Unit: spec.Unit}, nil
	}
	return MeasureResult{}, fmt.Errorf("unknown extract form %q", spec.Form)
}

// extractNumberValue takes the command's trimmed stdout as exactly one finite
// number. Empty, non-numeric, or multi-token stdout is a measurement failure.
func extractNumberValue(raw []byte, spec *ExtractSpec) (MeasureResult, error) {
	fields := strings.Fields(string(raw))
	if len(fields) != 1 {
		return MeasureResult{}, fmt.Errorf("extract number expects exactly one number on stdout, got %d token(s)", len(fields))
	}
	n, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return MeasureResult{}, fmt.Errorf("extract number: %q is not a finite number", fields[0])
	}
	return MeasureResult{Value: n, Unit: spec.Unit}, nil
}

// extractLineCount counts non-empty (trimmed) stdout lines — the "count the
// matches this command printed" case.
func extractLineCount(raw []byte, spec *ExtractSpec) MeasureResult {
	count := 0.0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return MeasureResult{Value: count, Unit: spec.Unit}
}

// extractByRegex applies the regexp to each non-empty stdout line. Every such
// line must match — an unmatched line is a measurement failure (the honesty
// guard against a mistyped pattern silently reporting zero). value = matching
// line count; a named `path` group (with optional `line` group) builds the
// per-file-count breakdown, +1 per matching line.
func extractByRegex(cfg *Config, raw []byte, spec *ExtractSpec) (MeasureResult, error) {
	pathIdx, lineIdx := -1, -1
	for i, name := range spec.Regex.SubexpNames() {
		switch name {
		case "path":
			pathIdx = i
		case "line":
			lineIdx = i
		}
	}
	findings := newFileFindings(cfg)
	count := 0.0
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSuffix(line, "\r") // CRLF stdout: match the logical line, not the bare \r
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := spec.Regex.FindStringSubmatch(line)
		if m == nil {
			return MeasureResult{}, fmt.Errorf("extract regex did not match line %q — filter noise out of the command or fix the pattern", line)
		}
		count++
		if pathIdx >= 0 {
			lineNo := 0
			if lineIdx >= 0 {
				lineNo, _ = strconv.Atoi(m[lineIdx])
			}
			findings.add(m[pathIdx], lineNo, 1)
		}
	}
	if pathIdx >= 0 {
		res := findings.result(spec.Unit)
		res.Value = count
		return res, nil
	}
	return MeasureResult{Value: count, Unit: spec.Unit}, nil
}
