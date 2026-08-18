package pawl

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// attachWatch fills rep.Watch with near/over files this invocation touched.
// It is advisory: a failure to resolve git state omits the field rather than
// failing the gate. record never carries watch.
func attachWatch(cfg *Config, rep *Report, scope *sinceScope) {
	if rep.Command != "check" {
		return
	}
	touched, err := touchedConfigPaths(cfg, scope)
	if err != nil || len(touched) == 0 {
		return
	}
	var watch []WatchEntry
	for _, d := range cfg.Dimensions {
		var (
			kind     string
			fallback int
			sizeOf   func([]byte) int
		)
		switch d.Builtin {
		case builtinFileLength:
			kind, fallback, sizeOf = "lines", defaultFileLengthThreshold, lineCount
		case builtinFileBytes:
			kind, fallback, sizeOf = "bytes", defaultFileBytesThreshold, func(data []byte) int { return len(data) }
		default:
			continue
		}
		watch = append(watch, watchTouched(cfg, d, kind, fallback, sizeOf, touched)...)
	}
	if len(watch) == 0 {
		return
	}
	seen := map[string]bool{}
	uniq := watch[:0]
	for _, w := range watch {
		key := w.Kind + "\x00" + w.Path
		if seen[key] {
			continue
		}
		seen[key] = true
		uniq = append(uniq, w)
	}
	watch = uniq
	sort.Slice(watch, func(i, j int) bool {
		if watch[i].Headroom != watch[j].Headroom {
			return watch[i].Headroom < watch[j].Headroom
		}
		if watch[i].Path != watch[j].Path {
			return watch[i].Path < watch[j].Path
		}
		return watch[i].ID < watch[j].ID
	})
	rep.Watch = watch
}

func watchTouched(cfg *Config, dim Dimension, kind string, fallback int, sizeOf func([]byte) int, touched map[string]bool) []WatchEntry {
	threshold := intOption(dim.Options, "threshold", fallback)
	include := stringList(dim.Options["include"])
	exclude := stringList(dim.Options["exclude"])
	nearFloor := float64(threshold) * rankNearRatio
	var out []WatchEntry
	for rel := range touched {
		if !matchesAny(include, rel) || matchesAny(exclude, rel) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cfg.Dir, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		n := sizeOf(data)
		status := "ok"
		if n > threshold {
			status = "over"
		} else if float64(n) > nearFloor {
			status = "near"
		} else {
			continue
		}
		out = append(out, WatchEntry{
			ID:        dim.ID,
			Path:      rel,
			Kind:      kind,
			Value:     float64(n),
			Threshold: float64(threshold),
			Headroom:  float64(threshold) - float64(n),
			Status:    status,
		})
	}
	return out
}

func touchedConfigPaths(cfg *Config, scope *sinceScope) (map[string]bool, error) {
	var repoPaths []string
	relDir := "."
	if scope != nil {
		relDir = scope.relDir
		for p := range scope.added {
			repoPaths = append(repoPaths, p)
		}
	} else {
		paths, err := dirtyRepoPaths(cfg.Dir)
		if err != nil {
			return nil, err
		}
		d, err := gitRelDir(cfg.Dir)
		if err != nil {
			return nil, err
		}
		relDir = d
		repoPaths = paths
	}
	touched := map[string]bool{}
	for _, repoRel := range repoPaths {
		if rel, ok := repoPathToConfig(repoRel, relDir); ok {
			touched[rel] = true
		}
	}
	return touched, nil
}

func dirtyRepoPaths(dir string) ([]string, error) {
	// Both listings are -z: a path is whatever bytes git prints between the
	// NULs, blanks at either end included. Splitting on newlines and trimming
	// would drop such a file out of the touched set and out of watch with it.
	listed, code, gitErr := gitOutputRaw(dir, "diff", "--name-only", "--no-renames", "-z", "HEAD")
	if code != 0 {
		return nil, fmt.Errorf("git diff failed: %s", gitErr)
	}
	seen := map[string]bool{}
	var paths []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		paths = append(paths, filepath.ToSlash(p))
	}
	for _, p := range strings.Split(listed, "\x00") {
		add(p)
	}
	others, code, gitErr := gitOutputRaw(dir, "ls-files", "--others", "--exclude-standard", "--full-name", "-z")
	if code != 0 {
		return nil, fmt.Errorf("git ls-files failed: %s", gitErr)
	}
	for _, p := range strings.Split(others, "\x00") {
		add(p)
	}
	return paths, nil
}

func gitRelDir(cfgDir string) (string, error) {
	toplevel, code, gitErr := gitOutput(cfgDir, "rev-parse", "--show-toplevel")
	if code != 0 {
		return "", fmt.Errorf("%s is not inside a git repository: %s", cfgDir, gitErr)
	}
	relDir, err := filepath.Rel(canonicalPath(toplevel), canonicalPath(cfgDir))
	if err != nil || relDir == ".." || strings.HasPrefix(relDir, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("config dir %s is outside the git repository %s", cfgDir, toplevel)
	}
	return filepath.ToSlash(relDir), nil
}

func repoPathToConfig(repoRel, relDir string) (string, bool) {
	repoRel = filepath.ToSlash(repoRel)
	if repoRel == "" {
		return "", false
	}
	if relDir == "." || relDir == "" {
		return repoRel, true
	}
	prefix := relDir + "/"
	if strings.HasPrefix(repoRel, prefix) {
		return strings.TrimPrefix(repoRel, prefix), true
	}
	return "", false
}
