package pawl

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"unicode"
)

func coverageSourcePaths(root string, sourceRoots []string, source string) ([]string, error) {
	return coverageSourcePathsWithResolver(root, sourceRoots, source, newCoveragePathResolver())
}

func coverageSourcePathsWithResolver(root string, sourceRoots []string, source string, resolver *coveragePathResolver) ([]string, error) {
	source = strings.TrimSpace(source)
	if source == "" || isForeignAbsoluteCoveragePath(source) {
		return nil, nil
	}
	source = filepath.FromSlash(source)
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = canonicalCoveragePathWithResolver(filepath.Clean(root), resolver)
	if err != nil {
		return nil, err
	}
	bases := sourceRoots
	if filepath.IsAbs(source) {
		bases = []string{""}
	} else if len(bases) == 0 {
		bases = []string{root}
	}
	paths := map[string]struct{}{}
	for _, base := range bases {
		base = strings.TrimSpace(base)
		if isForeignAbsoluteCoveragePath(base) {
			continue
		}
		base = filepath.FromSlash(base)
		if filepath.IsAbs(source) {
			base = ""
		} else if !filepath.IsAbs(base) {
			base = filepath.Join(root, base)
		}
		candidate := filepath.Clean(filepath.Join(base, source))
		candidate, inRoot, err := coverageContainedCandidate(root, candidate, resolver)
		if err != nil {
			return nil, err
		}
		if !inRoot {
			continue
		}
		if runtime.GOOS != "windows" && strings.Contains(source, `\`) {
			alternate := filepath.Clean(filepath.Join(base, strings.ReplaceAll(source, `\`, "/")))
			literal := canonicalPath(candidate)
			literal, literalInRoot, literalContainmentErr := coverageContainedCandidate(root, literal, resolver)
			if literalContainmentErr != nil {
				return nil, literalContainmentErr
			}
			alternate = canonicalPath(alternate)
			alternate, alternateInRoot, err := coverageContainedCandidate(root, alternate, resolver)
			if err != nil {
				return nil, err
			}
			var literalInfo os.FileInfo
			var literalExists bool
			if literalInRoot {
				var literalErr error
				literalInfo, literalExists, literalErr = resolver.statResult(literal)
				if literalErr != nil {
					return nil, fmt.Errorf("could not verify backslash path candidate %q: %w", literal, literalErr)
				}
			}
			var alternateInfo os.FileInfo
			var alternateExists bool
			if alternateInRoot {
				var alternateErr error
				alternateInfo, alternateExists, alternateErr = resolver.statResult(alternate)
				if alternateErr != nil {
					return nil, fmt.Errorf("could not verify slash path candidate %q: %w", alternate, alternateErr)
				}
			}
			literalExists = literalExists && literalInfo.Mode().IsRegular()
			alternateExists = alternateExists && alternateInfo.Mode().IsRegular()
			if literalExists && alternateExists && !os.SameFile(literalInfo, alternateInfo) {
				return nil, fmt.Errorf("ambiguous backslash path %q resolves to multiple files", source)
			}
			if alternateExists {
				candidate = alternate
			} else if literalInRoot {
				candidate = literal
			} else {
				continue
			}
		}
		resolved, err := canonicalCoveragePathWithResolver(candidate, resolver)
		if err != nil {
			return nil, err
		}
		_, stillInRoot, err := coverageContainedCandidate(root, resolved, resolver)
		if err != nil {
			return nil, err
		}
		if !stillInRoot {
			continue
		}
		paths[resolved] = struct{}{}
	}
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	return out, nil
}

func coverageContainedCandidate(root, candidate string, resolver *coveragePathResolver) (string, bool, error) {
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", false, err
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(root, abs)
	if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return abs, true, nil
	}
	if len(abs) <= len(root) || !strings.EqualFold(abs[:len(root)], root) || !strings.HasPrefix(abs[len(root):], string(filepath.Separator)) {
		return abs, false, nil
	}
	rootInfo, rootExists, rootErr := resolver.statResult(root)
	if rootErr != nil {
		return "", false, fmt.Errorf("could not verify coverage root %q: %w", root, rootErr)
	}
	aliasRoot := abs[:len(root)]
	aliasInfo, aliasExists, aliasErr := resolver.statResult(aliasRoot)
	if aliasErr != nil {
		return "", false, fmt.Errorf("could not verify candidate root %q: %w", aliasRoot, aliasErr)
	}
	if rootExists && aliasExists && os.SameFile(rootInfo, aliasInfo) {
		return filepath.Join(root, abs[len(root)+1:]), true, nil
	}
	return abs, false, nil
}

func isForeignAbsoluteCoveragePath(path string) bool {
	return isForeignAbsoluteCoveragePathForOS(path, runtime.GOOS)
}

func isForeignAbsoluteCoveragePathForOS(path, goos string) bool {
	path = strings.TrimSpace(path)
	driveAbsolute := len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (path[2] == '/' || path[2] == '\\')
	uncAbsolute := strings.HasPrefix(path, `\\`)
	if goos == "windows" {
		return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") || isWindowsRootedOrDriveRelative(path)
	}
	return driveAbsolute || uncAbsolute || isWindowsRootedOrDriveRelative(path)
}

func isWindowsRootedOrDriveRelative(path string) bool {
	return strings.HasPrefix(path, `\`) && !strings.HasPrefix(path, `\\`) || isWindowsDriveRelative(path)
}

func isWindowsDriveRelative(path string) bool {
	return len(path) >= 2 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (len(path) == 2 || path[2] != '/' && path[2] != '\\')
}

type coveragePathResolver struct {
	readDir  func(string) ([]os.DirEntry, error)
	stat     func(string) (os.FileInfo, error)
	dirs     map[string]coverageDirectoryIndex
	stats    map[string]coverageStatResult
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
		dirs:     map[string]coverageDirectoryIndex{},
		stats:    map[string]coverageStatResult{},
		identity: map[string]string{},
		fallback: map[string][]coverageFileIdentity{},
	}
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
	path = canonicalPath(path)
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
