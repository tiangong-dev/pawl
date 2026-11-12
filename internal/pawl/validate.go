package pawl

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// validateReport is the `pawl validate --format json` document: one object on
// stdout, always carrying every field so a consumer (an editor integration, a
// pre-commit hook, CI) can decide from the document alone.
type validateReport struct {
	SchemaVersion int    `json:"schema_version"`
	Command       string `json:"command"`
	// OK is the verdict the exit code carries: no config error, no stray key.
	OK bool `json:"ok"`
	// Config is the config path as it was given on the command line.
	Config      string             `json:"config"`
	Dimensions  int                `json:"dimensions"`
	Analyzers   int                `json:"analyzers"`
	Errors      []validateProblem  `json:"errors"`
	UnknownKeys []unknownConfigKey `json:"unknown_keys"`
}

// validateProblem is one config error, in the wording the measuring commands
// print — the same LoadConfig failure, so `validate` and `check` never
// disagree about what is wrong.
type validateProblem struct {
	Message string `json:"message"`
}

// runValidate loads the config and reports whether it is usable, without
// measuring anything: no adapter runs, no snapshot is read or written, no git
// ref is resolved, no file is created. It is the fast pre-flight for a config
// edit — and unlike every measuring command, a key the schema does not define
// is a problem here rather than an advisory warning.
// See spec/commands/validate.md.
func runValidate(configPath, format string, stdout, stderr io.Writer) int {
	rep := validateReport{
		SchemaVersion: 1,
		Command:       "validate",
		Config:        configPath,
		Errors:        []validateProblem{},
		UnknownKeys:   []unknownConfigKey{},
	}
	// The parse inside LoadConfig drops a key it does not know, so a stray key
	// is only visible in the raw document — read it here, in the same resolution
	// LoadConfig uses. A read failure needs no report of its own: LoadConfig
	// fails on it too and names the path in its own words below.
	if abs, err := filepath.Abs(configPath); err == nil {
		if data, err := os.ReadFile(abs); err == nil {
			rep.UnknownKeys = append(rep.UnknownKeys, findUnknownConfigKeys(data)...)
		}
	}
	// The config warning is discarded rather than printed: it advises that a
	// stray key is ignored, which is exactly what validate does not do.
	cfg, err := LoadConfig(configPath, io.Discard)
	if err != nil {
		rep.Errors = append(rep.Errors, validateProblem{Message: err.Error()})
	} else {
		rep.Dimensions = len(cfg.Dimensions)
		rep.Analyzers = len(cfg.Analyzers)
	}
	rep.OK = len(rep.Errors) == 0 && len(rep.UnknownKeys) == 0
	return renderValidate(rep, format, stdout, stderr)
}

// renderValidate prints the report — to stdout in both formats, because the
// report is the command's whole output: nothing here is progress, and nothing
// is diagnostic chatter next to a machine-readable document. The exit code is
// the machine signal: 0 valid, 2 anything found.
func renderValidate(rep validateReport, format string, stdout, stderr io.Writer) int {
	if format == "json" {
		data, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		fmt.Fprintf(stdout, "%s\n", data)
	} else if rep.OK {
		fmt.Fprintf(stdout, "ok: %s (%d dimension(s), %d analyzer(s))\n",
			rep.Config, rep.Dimensions, rep.Analyzers)
	} else {
		for _, e := range rep.Errors {
			fmt.Fprintln(stdout, e.Message)
		}
		for _, k := range rep.UnknownKeys {
			fmt.Fprintln(stdout, k.message())
		}
	}
	if rep.OK {
		return 0
	}
	return 2
}
