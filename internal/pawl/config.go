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
	default:
		return fmt.Errorf("unknown builtin %q (available: %s, %s, %s, %s, %s, %s)",
			builtin, builtinFileLength, builtinPatternCount, builtinEslint, builtinJscpd, builtinSwiftComplexity, builtinJSONValue)
	}
	return nil
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
