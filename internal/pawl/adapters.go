package pawl

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

// Shared adapter infrastructure. Every adapter that reports offenders produces
// the same `path:line` breakdown shape, and every adapter that reads a tool's
// JSON navigates to one number — so the path relativization, the breakdown
// accumulation, and the fail-loud JSON navigation live here once instead of in
// each adapter.

// relativizeToConfigDir turns an adapter-reported path into the config-relative,
// slash-separated form the breakdown keys use. Absolute paths are canonicalized
// on both sides first: a tool may report a path through a different symlink
// spelling than the config dir (macOS /var vs /private/var), and a naive Rel
// would produce ../../ garbage. An already-relative path is returned unchanged.
func relativizeToConfigDir(cfg *Config, path string) string {
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(canonicalPath(cfg.Dir), canonicalPath(path)); err == nil {
			path = rel
		}
	}
	return filepath.ToSlash(path)
}

// fileFindings accumulates per-file:line offender counts into the {value,
// breakdown} shape that per-file-count gating reads. eslint, swift-complexity,
// and pattern-count all emit this exact shape; centralizing it keeps the
// `path:line` key format and relativization single-sourced.
type fileFindings struct {
	cfg       *Config
	total     float64
	breakdown map[string]float64
}

func newFileFindings(cfg *Config) *fileFindings {
	return &fileFindings{cfg: cfg, breakdown: map[string]float64{}}
}

// add records count offenders at (path, line). path may be absolute or already
// config-relative; line uses the tool's 1-based numbering (0 when unknown).
func (f *fileFindings) add(path string, line int, count float64) {
	key := fmt.Sprintf("%s:%d", relativizeToConfigDir(f.cfg, path), line)
	f.breakdown[key] += count
	f.total += count
}

func (f *fileFindings) result(unit string) MeasureResult {
	return MeasureResult{Value: f.total, Unit: unit, Breakdown: f.breakdown}
}

// jsonNumberAtPath navigates a dotted key path (e.g.
// "statistics.total.duplicatedLines") into decoded JSON and returns the finite
// number there. A malformed document, a missing key, a non-object midway, or a
// non-numeric/non-finite leaf is an error — never a silent zero. This is the
// single source of truth for "read one number out of a tool's JSON", shared by
// the jscpd and json-value builtins.
func jsonNumberAtPath(data []byte, path string) (float64, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return 0, fmt.Errorf("not valid JSON: %v", err)
	}
	if path == "" {
		return 0, fmt.Errorf("empty json path")
	}
	segments := strings.Split(path, ".")
	cur := root
	for i, seg := range segments {
		obj, ok := cur.(map[string]any)
		if !ok {
			where := strings.Join(segments[:i], ".")
			if where == "" {
				where = "(root)"
			}
			return 0, fmt.Errorf("path %q: %s is not an object", path, where)
		}
		next, ok := obj[seg]
		if !ok {
			return 0, fmt.Errorf("path %q: key %q not found", path, strings.Join(segments[:i+1], "."))
		}
		cur = next
	}
	n, ok := cur.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, fmt.Errorf("path %q: value is not a finite number", path)
	}
	return n, nil
}

// canonicalPath resolves symlinks over the longest existing prefix of p and
// rejoins the non-existing remainder, so two spellings of the same location
// (macOS /var vs /private/var) compare equal even for paths that don't exist
// on disk.
func canonicalPath(p string) string {
	remainder := ""
	cur := p
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(resolved, remainder)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		remainder = filepath.Join(filepath.Base(cur), remainder)
		cur = parent
	}
}
