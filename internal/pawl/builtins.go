package pawl

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

const (
	builtinFileLength   = "file-length"
	builtinPatternCount = "pattern-count"
)

const defaultFileLengthThreshold = 500

func measureBuiltin(cfg *Config, dim Dimension, stderr io.Writer) (MeasureResult, error) {
	switch dim.Builtin {
	case builtinFileLength:
		return measureFileLength(cfg, dim)
	case builtinPatternCount:
		return measurePatternCount(cfg, dim)
	case builtinEslint:
		return measureEslint(cfg, dim, stderr)
	case builtinJscpd:
		return measureJscpd(cfg, dim, stderr)
	case builtinSwiftComplexity:
		return measureSwiftComplexity(cfg, dim, stderr)
	case builtinJSONValue:
		return measureJSONValue(cfg, dim, stderr)
	case builtinSarif:
		return measureSarif(cfg, dim, stderr)
	case builtinJUnit:
		return measureJUnit(cfg, dim, stderr)
	case builtinCoverage:
		return measureCoverage(cfg, dim, stderr)
	}
	// Unknown builtins are rejected at config load; reaching here is a bug.
	return MeasureResult{}, fmt.Errorf("unknown builtin %q", dim.Builtin)
}

// measureFileLength counts files whose line count exceeds the threshold.
// Long files are a maintainability smell and a direct cost to AI-assisted
// development: an over-long file burns the context window before any work
// begins. Pure filesystem scan — survives any linter/build change.
func measureFileLength(cfg *Config, dim Dimension) (MeasureResult, error) {
	threshold := intOption(dim.Options, "threshold", defaultFileLengthThreshold)
	include := stringList(dim.Options["include"])
	exclude := stringList(dim.Options["exclude"])

	breakdown := map[string]float64{}
	count := 0.0
	err := walkIncluded(cfg.Dir, include, exclude, func(rel, abs string) error {
		data, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		lines := lineCount(data)
		if lines > threshold {
			breakdown[rel] = float64(lines)
			count++
		}
		return nil
	})
	if err != nil {
		return MeasureResult{}, err
	}
	return MeasureResult{
		Value:     count,
		Unit:      fmt.Sprintf("files > %d lines", threshold),
		Breakdown: breakdown,
	}, nil
}

// measurePatternCount counts regexp matches across files — the generic
// suppression / escape-hatch counter (//nolint, @Suppress, !!, as!). The
// "path:line" breakdown keys are what make per-file-count gating work.
func measurePatternCount(cfg *Config, dim Dimension) (MeasureResult, error) {
	pattern, _ := dim.Options["pattern"].(string)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return MeasureResult{}, fmt.Errorf("pattern: %v", err)
	}
	include := stringList(dim.Options["include"])
	exclude := stringList(dim.Options["exclude"])

	findings := newFileFindings(cfg)
	err = walkIncluded(cfg.Dir, include, exclude, func(rel, abs string) error {
		data, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			matches := len(re.FindAllStringIndex(line, -1))
			if matches > 0 {
				findings.add(rel, i+1, float64(matches))
			}
		}
		return nil
	})
	if err != nil {
		return MeasureResult{}, err
	}
	return findings.result("matches"), nil
}

// walkIncluded visits every regular file under root whose slash-relative
// path matches at least one include glob and no exclude glob. `**` matches
// zero or more path segments. The .git directory is always skipped, and a
// directory whose own path matches an exclude glob (or a glob's `/**`-less
// prefix, the canonical directory-exclude form) is pruned without descending
// — excluding node_modules must cost zero traversal, not a match-per-file.
func walkIncluded(root string, include, exclude []string, visit func(rel, abs string) error) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			if path != root {
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				if excludesDir(exclude, filepath.ToSlash(rel)) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !matchesAny(include, rel) || matchesAny(exclude, rel) {
			return nil
		}
		return visit(rel, path)
	})
}

// excludesDir reports whether a directory can be pruned outright: its path
// matches an exclude glob directly, or matches a glob's prefix once the
// trailing `/**` is stripped (`**/node_modules/**` prunes at the
// `node_modules` directory itself).
func excludesDir(exclude []string, rel string) bool {
	for _, g := range exclude {
		if ok, err := doublestar.Match(g, rel); err == nil && ok {
			return true
		}
		if prefix, found := strings.CutSuffix(g, "/**"); found {
			if ok, err := doublestar.Match(prefix, rel); err == nil && ok {
				return true
			}
		}
	}
	return false
}

func matchesAny(globs []string, rel string) bool {
	for _, g := range globs {
		if ok, err := doublestar.Match(g, rel); err == nil && ok {
			return true
		}
	}
	return false
}

// lineCount mirrors the pawl line-count contract: an empty file is 0
// lines, and a trailing newline does not add a phantom line.
func lineCount(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := 1
	for _, b := range data {
		if b == '\n' {
			n++
		}
	}
	if data[len(data)-1] == '\n' {
		n--
	}
	return n
}

func intOption(options map[string]any, key string, fallback int) int {
	switch v := options[key].(type) {
	case int64:
		return int(v)
	case int:
		return v
	case float64:
		return int(v)
	}
	return fallback
}

// numberOption reads a numeric option, reporting presence so a required
// threshold can be distinguished from an absent one at validation time.
func numberOption(options map[string]any, key string) (float64, bool) {
	switch v := options[key].(type) {
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case float64:
		return v, true
	}
	return 0, false
}
