package pawl

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	sourceWasAbsolute := filepath.IsAbs(source)
	if sourceWasAbsolute {
		bases = []string{""}
	} else if len(bases) == 0 {
		bases = []string{root}
	}
	paths := map[string]struct{}{}
	for _, rawBase := range bases {
		rawBase = strings.TrimSpace(rawBase)
		if isForeignAbsoluteCoveragePath(rawBase) {
			continue
		}
		baseWasAbsolute := filepath.IsAbs(filepath.FromSlash(rawBase))
		baseVariants := []string{filepath.FromSlash(rawBase)}
		sourceVariants := []string{source}
		if runtime.GOOS != "windows" {
			if strings.Contains(rawBase, `\`) {
				baseVariants = append(baseVariants, filepath.FromSlash(strings.ReplaceAll(rawBase, `\`, "/")))
			}
			if strings.Contains(source, `\`) {
				sourceVariants = append(sourceVariants, strings.ReplaceAll(source, `\`, "/"))
			}
		}
		if sourceWasAbsolute {
			baseVariants = []string{""}
		}
		candidates := make([]string, 0, len(baseVariants)*len(sourceVariants))
		preferred := ""
		for baseIndex, base := range baseVariants {
			if !sourceWasAbsolute && !filepath.IsAbs(base) {
				base = filepath.Join(root, base)
			}
			for sourceIndex, name := range sourceVariants {
				candidate := filepath.Clean(filepath.Join(base, name))
				if baseIndex == 0 && sourceIndex == 0 {
					preferred = candidate
				}
				candidates = append(candidates, candidate)
			}
		}
		resolved, err := resolveCoverageCandidateVariants(root, candidates, preferred, sourceWasAbsolute || baseWasAbsolute, resolver)
		if err != nil {
			return nil, err
		}
		if resolved != "" {
			paths[resolved] = struct{}{}
		}
	}
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	return out, nil
}

func resolveCoverageCandidateVariants(root string, candidates []string, preferred string, resolveAlias bool, resolver *coveragePathResolver) (string, error) {
	var existing []string
	var preferredPath string
	for _, candidate := range candidates {
		isPreferred := filepath.Clean(candidate) == filepath.Clean(preferred)
		candidate, inRoot, err := coverageContainedCandidate(root, candidate, resolver, resolveAlias)
		if err != nil {
			return "", err
		}
		if !inRoot {
			continue
		}
		canonical, err := resolveCoveragePath(candidate, resolver)
		if err != nil {
			return "", err
		}
		canonical, inRoot, err = coverageContainedCandidate(root, canonical, resolver, false)
		if err != nil {
			return "", err
		}
		if !inRoot {
			continue
		}
		if isPreferred {
			preferredPath = canonical
		}
		info, exists, err := resolver.statResult(canonical)
		if err != nil {
			return "", fmt.Errorf("could not verify coverage source candidate %q: %w", canonical, err)
		}
		if !exists || !info.Mode().IsRegular() {
			continue
		}
		resolved, err := canonicalCoveragePathWithResolver(canonical, resolver)
		if err != nil {
			return "", err
		}
		existing = append(existing, resolved)
	}
	unique := make(map[string]struct{}, len(existing))
	for _, path := range existing {
		unique[path] = struct{}{}
	}
	if len(unique) > 1 {
		return "", fmt.Errorf("ambiguous backslash path %q resolves to multiple in-root files", preferred)
	}
	for path := range unique {
		return path, nil
	}
	if preferredPath != "" {
		return preferredPath, nil
	}
	return "", nil
}

func coverageContainedCandidate(root, candidate string, resolver *coveragePathResolver, resolveAlias bool) (string, bool, error) {
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", false, err
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(root, abs)
	if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return abs, true, nil
	}
	if resolveAlias {
		canonical, canonicalErr := resolveCoveragePath(abs, resolver)
		if canonicalErr != nil {
			return "", false, canonicalErr
		}
		if rel, err := filepath.Rel(root, canonical); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return canonical, true, nil
		}
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
