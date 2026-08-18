package pawl

import (
	"fmt"
	"strings"
	"time"
)

// Analyzer is a named, explicitly shareable tool invocation. It acquires and
// decodes one complete finding report; dimensions reference it with source and
// apply independent rule/level selectors without rerunning the tool.
type Analyzer struct {
	ID      string
	Builtin string
	Timeout time.Duration
	Options map[string]any
	Verify  []string
}

type analyzerConfig struct {
	ID      string         `yaml:"id"`
	Builtin string         `yaml:"builtin"`
	Timeout string         `yaml:"timeout"`
	Options map[string]any `yaml:"options"`
	Verify  []string       `yaml:"verify"`
}

func validateAnalyzer(index int, raw analyzerConfig) (Analyzer, error) {
	id := raw.ID
	if id == "" {
		id = fmt.Sprintf("#%d", index+1)
		return Analyzer{}, fmt.Errorf("analyzer %s: missing id", id)
	}
	if raw.Builtin != builtinEslint && raw.Builtin != builtinOxlint && raw.Builtin != builtinSarif {
		return Analyzer{}, fmt.Errorf("analyzer %s: builtin must be %q, %q or %q, got %q",
			id, builtinEslint, builtinOxlint, builtinSarif, raw.Builtin)
	}
	timeout := defaultTimeout
	if raw.Timeout != "" {
		parsed, err := time.ParseDuration(raw.Timeout)
		if err != nil || parsed <= 0 {
			return Analyzer{}, fmt.Errorf("analyzer %s: timeout %q is not a positive duration", id, raw.Timeout)
		}
		timeout = parsed
	}
	if err := validateBuiltinOptions(raw.Builtin, raw.Options); err != nil {
		return Analyzer{}, fmt.Errorf("analyzer %s: %v", id, err)
	}
	allowedOptions := map[string]bool{"command": true, "min_files": true}
	if raw.Builtin == builtinSarif {
		allowedOptions["file"] = true
		allowedOptions["valid_exit_codes"] = true
		allowedOptions["verify_rules"] = true
	}
	for option := range raw.Options {
		if !allowedOptions[option] {
			return Analyzer{}, fmt.Errorf("analyzer %s: option %q is not valid for a named %s analyzer", id, option, raw.Builtin)
		}
	}
	if value, exists := raw.Options["min_files"]; exists {
		n, ok := numberOption(raw.Options, "min_files")
		if !ok || n < 0 || n != float64(int(n)) {
			return Analyzer{}, fmt.Errorf("analyzer %s: min_files must be a non-negative integer, got %v", id, value)
		}
	}
	if len(raw.Verify) > 0 && raw.Builtin != builtinEslint && raw.Builtin != builtinOxlint {
		return Analyzer{}, fmt.Errorf("analyzer %s: verify is supported only for eslint or oxlint", id)
	}
	for _, command := range raw.Verify {
		if strings.TrimSpace(command) == "" {
			return Analyzer{}, fmt.Errorf("analyzer %s: verify commands must not be empty", id)
		}
	}
	if raw.Builtin == builtinSarif {
		if _, err := exitCodeSet(raw.Options["valid_exit_codes"]); err != nil {
			return Analyzer{}, fmt.Errorf("analyzer %s: valid_exit_codes: %v", id, err)
		}
		if _, exists := raw.Options["valid_exit_codes"]; exists {
			if command, _ := raw.Options["command"].(string); command == "" {
				return Analyzer{}, fmt.Errorf("analyzer %s: valid_exit_codes requires a command", id)
			}
		}
		if value, exists := raw.Options["verify_rules"]; exists {
			if _, ok := value.(bool); !ok {
				return Analyzer{}, fmt.Errorf("analyzer %s: verify_rules must be a boolean", id)
			}
		}
	}
	return Analyzer{ID: raw.ID, Builtin: raw.Builtin, Timeout: timeout, Options: raw.Options, Verify: raw.Verify}, nil
}

func validateAnalyzerSelector(builtin string, options map[string]any) error {
	allowed := map[string]bool{"rules": true}
	if builtin == builtinSarif || builtin == builtinOxlint {
		allowed["levels"] = true
	}
	for option := range options {
		if !allowed[option] {
			return fmt.Errorf("option %q is not a valid %s selector", option, builtin)
		}
	}
	if _, err := strictStringList(options["rules"]); err != nil {
		return fmt.Errorf("rules: %v", err)
	}
	if builtin == builtinEslint {
		return nil
	}
	levels, err := strictStringList(options["levels"])
	if err != nil {
		return fmt.Errorf("levels: %v", err)
	}
	for _, lv := range levels {
		if !validAnalyzerLevel(builtin, lv) {
			return fmt.Errorf("levels entry %q must be one of %s", lv, analyzerLevelsDescription(builtin))
		}
	}
	return nil
}

func validAnalyzerLevel(builtin, level string) bool {
	if builtin == builtinOxlint {
		return level == "error" || level == "warning" || level == "advice"
	}
	switch level {
	case "error", "warning", "note", "none":
		return true
	default:
		return false
	}
}

func analyzerLevelsDescription(builtin string) string {
	if builtin == builtinOxlint {
		return "error, warning or advice"
	}
	return "error, warning, note or none"
}

// exitCodeSet validates a declared successful-exit contract into a set. The nil
// map means nothing was declared, which every caller reads as "exit 0 or bust".
func exitCodeSet(v any) (map[int]bool, error) {
	if v == nil {
		return nil, nil
	}
	items, ok := v.([]any)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("must be a non-empty list of integers")
	}
	out := make(map[int]bool, len(items))
	for _, item := range items {
		var code int
		switch n := item.(type) {
		case int:
			code = n
		case int64:
			code = int(n)
		case float64:
			if n != float64(int(n)) {
				return nil, fmt.Errorf("every entry must be an integer, got %v", n)
			}
			code = int(n)
		default:
			return nil, fmt.Errorf("every entry must be an integer, got %T", item)
		}
		if code < 0 || code > 255 {
			return nil, fmt.Errorf("entry %d must be between 0 and 255", code)
		}
		out[code] = true
	}
	return out, nil
}
