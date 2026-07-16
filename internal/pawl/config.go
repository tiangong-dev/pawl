package pawl

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultSnapshotName = "pawl.snapshot.json"
const defaultTimeout = 10 * time.Minute

// Dimension is one configured quality dimension, validated and ready to
// measure. Exactly one of Command / Builtin is set. Extract is set only on a
// Command dimension that derives its measurement from raw output declaratively.
type Dimension struct {
	ID        string
	Title     string
	Direction Direction
	Gate      GateMode
	Tolerance *float64
	Timeout   time.Duration
	Command   string
	Builtin   string
	Options   map[string]any
	Extract   *ExtractSpec
}

// GateSpecOf is the dimension's comparison contract for RegressionsOf.
func (d Dimension) GateSpecOf() GateSpec {
	tolerance := 0.0
	if d.Tolerance != nil {
		tolerance = *d.Tolerance
	}
	return GateSpec{Direction: d.Direction, Gate: d.Gate, Tolerance: tolerance}
}

// Config is a validated pawl.yaml: where the snapshot lives and what to
// measure. Dir is the config file's directory — the root every relative
// path, glob, and adapter cwd resolves against.
type Config struct {
	Dir          string
	SnapshotPath string
	Dimensions   []Dimension
}

type configFile struct {
	Snapshot   string            `yaml:"snapshot"`
	Dimensions []dimensionConfig `yaml:"dimensions"`
}

type dimensionConfig struct {
	ID        string         `yaml:"id"`
	Title     string         `yaml:"title"`
	Direction string         `yaml:"direction"`
	Gate      string         `yaml:"gate"`
	Tolerance *float64       `yaml:"tolerance"`
	Timeout   string         `yaml:"timeout"`
	Command   string         `yaml:"command"`
	Builtin   string         `yaml:"builtin"`
	Options   map[string]any `yaml:"options"`
	Extract   any            `yaml:"extract"`
}

// LoadConfig reads and validates a pawl.yaml. Every validation failure is an
// error — a config that cannot be trusted must abort with exit 2, never
// half-run.
func LoadConfig(path string) (*Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving config path %s: %w", path, err)
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("config file %s: %w", path, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var raw configFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(raw.Dimensions) == 0 {
		return nil, fmt.Errorf("%s declares no dimensions — nothing to measure", path)
	}

	dir := filepath.Dir(abs)
	snapshotRel := raw.Snapshot
	if snapshotRel == "" {
		snapshotRel = defaultSnapshotName
	}

	cfg := &Config{Dir: dir, SnapshotPath: filepath.Join(dir, snapshotRel)}
	seen := map[string]bool{}
	for i, d := range raw.Dimensions {
		dim, err := validateDimension(i, d)
		if err != nil {
			return nil, err
		}
		if seen[dim.ID] {
			return nil, fmt.Errorf("duplicate dimension id %q", dim.ID)
		}
		seen[dim.ID] = true
		cfg.Dimensions = append(cfg.Dimensions, dim)
	}
	return cfg, nil
}

func validateDimension(index int, d dimensionConfig) (Dimension, error) {
	fail := func(format string, args ...any) (Dimension, error) {
		id := d.ID
		if id == "" {
			id = fmt.Sprintf("#%d", index+1)
		}
		return Dimension{}, fmt.Errorf("dimension %s: %s", id, fmt.Sprintf(format, args...))
	}

	if d.ID == "" {
		return fail("missing id")
	}
	if d.Title == "" {
		return fail("missing title")
	}
	direction := Direction(d.Direction)
	if direction != LowerIsBetter && direction != HigherIsBetter {
		return fail("direction must be %q or %q, got %q", LowerIsBetter, HigherIsBetter, d.Direction)
	}
	gate := GateMode(d.Gate)
	switch gate {
	case "", GateTotal, GatePerFileCount, GatePerKeyValue:
	default:
		return fail("gate must be %q, %q or %q, got %q", GateTotal, GatePerFileCount, GatePerKeyValue, d.Gate)
	}
	timeout := defaultTimeout
	if d.Timeout != "" {
		parsed, err := time.ParseDuration(d.Timeout)
		if err != nil || parsed <= 0 {
			return fail("timeout %q is not a positive duration", d.Timeout)
		}
		timeout = parsed
	}
	if (d.Command == "") == (d.Builtin == "") {
		return fail("exactly one of command / builtin is required")
	}
	if d.Builtin != "" {
		if err := validateBuiltinOptions(d.Builtin, d.Options); err != nil {
			return fail("%s", err)
		}
	}
	var extract *ExtractSpec
	if d.Extract != nil {
		if d.Builtin != "" {
			return fail("extract is an exec-adapter feature and cannot be set on a builtin dimension")
		}
		spec, err := parseExtract(d.Extract)
		if err != nil {
			return fail("extract: %s", err)
		}
		extract = spec
	}
	return Dimension{
		ID:        d.ID,
		Title:     d.Title,
		Direction: direction,
		Gate:      gate,
		Tolerance: d.Tolerance,
		Timeout:   timeout,
		Command:   d.Command,
		Builtin:   d.Builtin,
		Options:   d.Options,
		Extract:   extract,
	}, nil
}

func validateBuiltinOptions(builtin string, options map[string]any) error {
	switch builtin {
	case builtinFileLength:
		if len(stringList(options["include"])) == 0 {
			return fmt.Errorf("builtin %q requires a non-empty include glob list", builtin)
		}
	case builtinPatternCount:
		pattern, _ := options["pattern"].(string)
		if pattern == "" {
			return fmt.Errorf("builtin %q requires a pattern", builtin)
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("builtin %q pattern: %v", builtin, err)
		}
		if len(stringList(options["include"])) == 0 {
			return fmt.Errorf("builtin %q requires a non-empty include glob list", builtin)
		}
	case builtinEslint:
		if command, _ := options["command"].(string); command == "" {
			return fmt.Errorf("builtin %q requires a command option (an eslint invocation producing --format json on stdout)", builtin)
		}
	case builtinJscpd:
		if command, _ := options["command"].(string); command == "" {
			return fmt.Errorf("builtin %q requires a command option (a jscpd invocation with --reporters json)", builtin)
		}
		if report, _ := options["report"].(string); report == "" {
			return fmt.Errorf("builtin %q requires a report option (path to the produced jscpd-report.json)", builtin)
		}
	case builtinSwiftComplexity:
		if command, _ := options["command"].(string); command == "" {
			return fmt.Errorf("builtin %q requires a command option (a swift-complexity invocation with --format json)", builtin)
		}
		if _, ok := numberOption(options, "threshold"); !ok {
			return fmt.Errorf("builtin %q requires a numeric threshold option", builtin)
		}
		if m, ok := options["metric"].(string); ok && m != "" && m != "cognitive" && m != "cyclomatic" {
			return fmt.Errorf("builtin %q metric must be %q or %q, got %q", builtin, "cognitive", "cyclomatic", m)
		}
	case builtinJSONValue:
		if path, _ := options["path"].(string); path == "" {
			return fmt.Errorf("builtin %q requires a path option (a dotted key path to a number)", builtin)
		}
		command, _ := options["command"].(string)
		file, _ := options["file"].(string)
		if command == "" && file == "" {
			return fmt.Errorf("builtin %q requires a command or a file option (the JSON source)", builtin)
		}
	case builtinSarif:
		command, _ := options["command"].(string)
		file, _ := options["file"].(string)
		if command == "" && file == "" {
			return fmt.Errorf("builtin %q requires a command or a file option (the SARIF source)", builtin)
		}
		// Strict, not stringList: a non-string entry (e.g. levels: [123]) must be
		// rejected, not silently dropped into "no filter" — that would broaden
		// the gate behind the user's back.
		levels, err := strictStringList(options["levels"])
		if err != nil {
			return fmt.Errorf("builtin %q levels: %v", builtin, err)
		}
		for _, lv := range levels {
			switch lv {
			case "error", "warning", "note", "none":
			default:
				return fmt.Errorf("builtin %q levels entry %q must be one of error, warning, note or none", builtin, lv)
			}
		}
		if _, err := strictStringList(options["rules"]); err != nil {
			return fmt.Errorf("builtin %q rules: %v", builtin, err)
		}
	case builtinJUnit:
		command, _ := options["command"].(string)
		file, _ := options["file"].(string)
		if command == "" && file == "" {
			return fmt.Errorf("builtin %q requires a command or a file option (the JUnit XML source)", builtin)
		}
		if c, ok := options["count"].(string); ok && c != "" {
			switch c {
			case "failures", "tests", "skipped", "passing":
			default:
				return fmt.Errorf("builtin %q count must be one of failures, tests, skipped or passing, got %q", builtin, c)
			}
		}
	case builtinCoverage:
		if file, _ := options["file"].(string); file == "" {
			return fmt.Errorf("builtin %q requires a file option (the coverage report)", builtin)
		}
		format, _ := options["format"].(string)
		switch format {
		case "lcov", "cobertura":
		default:
			return fmt.Errorf("builtin %q requires a format option of lcov or cobertura, got %q", builtin, format)
		}
		metric, _ := options["metric"].(string)
		switch metric {
		case "", "lines", "branches", "functions":
		default:
			return fmt.Errorf("builtin %q metric must be lines, branches or functions, got %q", builtin, metric)
		}
		if metric == "functions" && format == "cobertura" {
			return fmt.Errorf("builtin %q metric functions is lcov-only (cobertura has no functions rate)", builtin)
		}
	default:
		return fmt.Errorf("unknown builtin %q (available: %s, %s, %s, %s, %s, %s, %s, %s, %s)",
			builtin, builtinFileLength, builtinPatternCount, builtinEslint, builtinJscpd,
			builtinSwiftComplexity, builtinJSONValue, builtinSarif, builtinJUnit, builtinCoverage)
	}
	return nil
}

// strictStringList validates an optional list option: absent is fine (nil), but
// a present value must be a list of strings — a non-list, or any non-string
// entry, is an error rather than a silent drop. Used for filter lists whose
// silent broadening would weaken the gate unnoticed.
func strictStringList(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("must be a list of strings")
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("every entry must be a string, got %T", item)
		}
		out = append(out, s)
	}
	return out, nil
}

// stringList coerces a config array option into []string, dropping non-string
// entries (they fail include-required validation rather than half-matching).
func stringList(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
