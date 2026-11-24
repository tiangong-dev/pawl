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
// See spec/adapters/extract.md.

const (
	extractNumber   = "number"
	extractLines    = "lines"
	extractExitCode = "exit-code"
	extractRegex    = "regex"
	extractJSONPath = "json_path"
)

// extractExitCodeUnit is what an exit-code measurement counts: failed checks.
// The scalar form has no place to override it, so it is fixed here rather than
// left to the "count" default every other form inherits.
const extractExitCodeUnit = "failures"

// defaultExitCodeOk is `extract: exit-code`'s valid_exit_codes when the config
// declares none. For a pass/fail script the exit code *is* the result, so a
// failing check (1) is a measurement rather than a broken tool — but an exit
// outside the set (timeout, signal, missing binary) still is a failure to
// measure. Every other extract form keeps the plain "exit 0 or bust" default.
var defaultExitCodeOk = map[int]bool{0: true, 1: true}

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
		case extractExitCode:
			return &ExtractSpec{Form: e, Unit: extractExitCodeUnit}, nil
		default:
			return nil, fmt.Errorf("unknown form %q — want %q, %q, %q, or an object with %q/%q",
				e, extractNumber, extractLines, extractExitCode, extractRegex, extractJSONPath)
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
		return nil, fmt.Errorf("must be a string (%q/%q/%q) or an object (%q/%q)",
			extractNumber, extractLines, extractExitCode, extractRegex, extractJSONPath)
	}
}

// measureExtract runs the command and derives the measurement per the declared
// form. The command must exit 0 unless valid_exit_codes widens the contract; an
// unaccepted exit, a timeout, or output that cannot be extracted is a
// measurement failure (never a silent zero), matching the raw exec contract's
// honesty rule. `exit-code` inverts the roles: the exit code is the
// measurement, and the output is not read at all.
func measureExtract(cfg *Config, dim Dimension, stderr io.Writer) (MeasureResult, error) {
	stdout, exitCode, err := runAdapterCommand(cfg, dim, stderr, dim.Command)
	if err != nil {
		return MeasureResult{}, err
	}
	spec := dim.Extract
	if spec.Form == extractExitCode {
		return extractExitCodeValue(dim.OkExit, exitCode)
	}
	if !exitAccepted(dim.OkExit, exitCode) {
		return MeasureResult{}, exitCodeError(dim.OkExit, exitCode)
	}
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

// extractExitCodeValue turns a pass/fail script's exit code into the
// measurement: 0 means the check passed, any other accepted code means it
// reported failures. A code outside the accepted set (including a signal or a
// missing binary, which the shell reports as an ordinary exit) stays a
// measurement failure — "the tool never ran" must not read as "the check
// failed".
func extractExitCodeValue(ok map[int]bool, exitCode int) (MeasureResult, error) {
	if ok == nil {
		ok = defaultExitCodeOk
	}
	if !exitAccepted(ok, exitCode) {
		return MeasureResult{}, exitCodeError(ok, exitCode)
	}
	if exitCode == 0 {
		return MeasureResult{Value: 0, Unit: extractExitCodeUnit}, nil
	}
	return MeasureResult{Value: 1, Unit: extractExitCodeUnit}, nil
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
