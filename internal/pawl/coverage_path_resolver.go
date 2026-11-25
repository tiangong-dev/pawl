package pawl

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"unicode"
)

type coveragePathResolver struct {
	readDir  func(string) ([]os.DirEntry, error)
	stat     func(string) (os.FileInfo, error)
	lstat    func(string) (os.FileInfo, error)
	dirs     map[string]coverageDirectoryIndex
	stats    map[string]coverageStatResult
	lstats   map[string]coverageStatResult
	identity map[string]string
	fallback map[string][]coverageFileIdentity
}

type coverageStatResult struct {
	info os.FileInfo
	ok   bool
	err  error
}

type coverageFileIdentity struct {
	path string
	info os.FileInfo
}

type coverageDirectoryIndex struct {
	exact  map[string]string
	folded map[string][]string
	err    error
}

func newCoveragePathResolver() *coveragePathResolver {
	return &coveragePathResolver{
		readDir:  os.ReadDir,
		stat:     os.Stat,
		lstat:    os.Lstat,
		dirs:     map[string]coverageDirectoryIndex{},
		stats:    map[string]coverageStatResult{},
		lstats:   map[string]coverageStatResult{},
		identity: map[string]string{},
		fallback: map[string][]coverageFileIdentity{},
	}
}

func (r *coveragePathResolver) lstatResult(path string) (os.FileInfo, bool, error) {
	path = filepath.Clean(path)
	if result, ok := r.lstats[path]; ok {
		return result.info, result.ok, result.err
	}
	info, err := r.lstat(path)
	result := coverageStatResult{info: info}
	if err == nil {
		result.ok = true
	} else if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ENAMETOOLONG) {
		// A missing non-symlink component leaves an ordinary report path.
	} else {
		result.err = err
	}
	r.lstats[path] = result
	return result.info, result.ok, result.err
}

func (r *coveragePathResolver) statResult(path string) (os.FileInfo, bool, error) {
	path = filepath.Clean(path)
	if result, ok := r.stats[path]; ok {
		return result.info, result.ok, result.err
	}
	info, err := r.stat(path)
	result := coverageStatResult{info: info}
	if err == nil {
		result.ok = true
	} else if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ENAMETOOLONG) {
		// A path that cannot be represented as one filesystem entry is simply
		// not an existing literal filename candidate.
	} else {
		result.err = err
	}
	r.stats[path] = result
	return result.info, result.ok, result.err
}

func (r *coveragePathResolver) directory(path string) coverageDirectoryIndex {
	path = filepath.Clean(path)
	if index, ok := r.dirs[path]; ok {
		return index
	}
	index := coverageDirectoryIndex{exact: map[string]string{}, folded: map[string][]string{}}
	entries, err := r.readDir(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			index.err = err
		}
		r.dirs[path] = index
		return index
	}
	for _, entry := range entries {
		name := entry.Name()
		index.exact[name] = name
		index.folded[coveragePathFoldKey(name)] = append(index.folded[coveragePathFoldKey(name)], name)
	}
	r.dirs[path] = index
	return index
}

// canonicalCoveragePath also recovers the on-disk spelling of existing path
// components. Stat identity, rather than case folding, keeps behavior correct
// on case-sensitive filesystems while deduplicating aliases on insensitive ones.
func canonicalCoveragePath(path string) string {
	resolved, _ := canonicalCoveragePathWithResolver(path, newCoveragePathResolver())
	return resolved
}

func canonicalCoveragePathWithResolver(path string, resolver *coveragePathResolver) (string, error) {
	path, err := resolveCoveragePath(path, resolver)
	if err != nil {
		return "", err
	}
	vol := filepath.VolumeName(path)
	cur := vol + string(filepath.Separator)
	rest := strings.TrimPrefix(path[len(vol):], string(filepath.Separator))
	for _, part := range strings.Split(rest, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		next := filepath.Join(cur, part)
		want, exists, err := resolver.statResult(next)
		if err != nil {
			return "", fmt.Errorf("could not verify coverage path %q: %w", next, err)
		}
		if exists {
			index := resolver.directory(cur)
			if index.err != nil {
				return "", fmt.Errorf("could not read coverage path directory %q: %w", cur, index.err)
			}
			if exact, ok := index.exact[part]; ok {
				next = filepath.Join(cur, exact)
			} else {
				for _, alias := range index.folded[coveragePathFoldKey(part)] {
					if !strings.EqualFold(alias, part) {
						continue
					}
					info, aliasExists, aliasErr := resolver.statResult(filepath.Join(cur, alias))
					if aliasErr != nil {
						return "", fmt.Errorf("could not verify coverage path alias %q: %w", alias, aliasErr)
					}
					if aliasExists && os.SameFile(want, info) {
						next = filepath.Join(cur, alias)
						break
					}
				}
			}
			next = resolver.canonicalIdentity(next, want)
		}
		cur = next
	}
	return filepath.Clean(cur), nil
}

func resolveCoveragePath(path string, resolver *coveragePathResolver) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	vol := filepath.VolumeName(abs)
	cur := vol + string(filepath.Separator)
	rest := strings.TrimPrefix(abs[len(vol):], string(filepath.Separator))
	parts := strings.Split(rest, string(filepath.Separator))
	for index, part := range parts {
		if part == "" || part == "." {
			continue
		}
		next := filepath.Join(cur, part)
		info, exists, err := resolver.lstatResult(next)
		if err != nil {
			return "", fmt.Errorf("could not inspect coverage path %q: %w", next, err)
		}
		if !exists {
			for _, remaining := range parts[index+1:] {
				next = filepath.Join(next, remaining)
			}
			return filepath.Clean(next), nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(next)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
					return "", fmt.Errorf("coverage path contains an unresolved symlink %q: %w", next, err)
				}
				return "", fmt.Errorf("could not resolve coverage symlink %q: %w", next, err)
			}
			cur = resolved
			continue
		}
		cur = next
	}
	return filepath.Clean(cur), nil
}

func (r *coveragePathResolver) canonicalIdentity(path string, info os.FileInfo) string {
	if key, ok := coverageStableFileIdentity(info); ok {
		if first, exists := r.identity[key]; exists {
			return first
		}
		r.identity[key] = path
		return path
	}
	key := fmt.Sprintf("%d:%d:%d", info.Size(), info.Mode().Type(), info.ModTime().UnixNano())
	for _, seen := range r.fallback[key] {
		if os.SameFile(info, seen.info) {
			return seen.path
		}
	}
	r.fallback[key] = append(r.fallback[key], coverageFileIdentity{path: path, info: info})
	return path
}

func coverageStableFileIdentity(info os.FileInfo) (string, bool) {
	sys := reflect.ValueOf(info.Sys())
	if !sys.IsValid() {
		return "", false
	}
	if sys.Kind() == reflect.Pointer {
		sys = sys.Elem()
	}
	if !sys.IsValid() || sys.Kind() != reflect.Struct {
		return "", false
	}
	dev, ino := sys.FieldByName("Dev"), sys.FieldByName("Ino")
	if !dev.IsValid() || !ino.IsValid() {
		return "", false
	}
	return reflectIntegerString(dev) + ":" + reflectIntegerString(ino), true
}

func reflectIntegerString(value reflect.Value) string {
	switch value.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return fmt.Sprintf("u%d", value.Uint())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("i%d", value.Int())
	default:
		return "?"
	}
}

func coveragePathFoldKey(s string) string {
	var key strings.Builder
	for _, r := range s {
		min := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < min {
				min = next
			}
		}
		key.WriteRune(min)
	}
	return key.String()
}

func existingCoverageCandidates(paths []string) ([]string, error) {
	return existingCoverageCandidatesWithResolver(paths, newCoveragePathResolver())
}

func existingCoverageCandidatesWithResolver(paths []string, resolver *coveragePathResolver) ([]string, error) {
	var existing []string
	seen := map[string]struct{}{}
	for _, path := range paths {
		info, exists, err := resolver.statResult(path)
		if err != nil {
			return nil, fmt.Errorf("could not verify source candidate %q: %w", path, err)
		}
		if !exists || !info.Mode().IsRegular() {
			continue
		}
		canonical, err := canonicalCoveragePathWithResolver(path, resolver)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[canonical]; duplicate {
			continue
		}
		seen[canonical] = struct{}{}
		existing = append(existing, canonical)
	}
	return existing, nil
}
