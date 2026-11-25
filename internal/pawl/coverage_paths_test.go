package pawl

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestCoverageSourcePathsNormalizeAndStayWithinRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "src", "a.c")
	for _, source := range []string{"src/a.c", inside} {
		paths, err := coverageSourcePaths(root, nil, source)
		if err != nil {
			t.Fatalf("coverageSourcePaths(%q): %v", source, err)
		}
		if len(paths) != 1 || paths[0] != inside {
			t.Errorf("coverageSourcePaths(%q) = %v, want [%s]", source, paths, inside)
		}
	}
	for _, source := range []string{"../outside.c", filepath.Join(filepath.Dir(root), "outside.c")} {
		paths, err := coverageSourcePaths(root, nil, source)
		if err != nil {
			t.Fatalf("coverageSourcePaths(%q): %v", source, err)
		}
		if len(paths) != 0 {
			t.Errorf("coverageSourcePaths(%q) = %v, want no paths outside root", source, paths)
		}
	}
}

func TestCoverageSourcePathsCanonicalizeSymlinksAndRejectEscapes(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	realFile := filepath.Join(root, "src", "a.c")
	if err := os.MkdirAll(filepath.Dir(realFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	rootAlias := filepath.Join(parent, "repo-alias")
	if err := os.Symlink(root, rootAlias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	paths, err := coverageSourcePaths(rootAlias, nil, realFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != canonicalPath(realFile) {
		t.Errorf("real path under symlinked config root = %v, want [%s]", paths, canonicalPath(realFile))
	}

	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	paths, err = coverageSourcePaths(root, nil, "linked/secret.c")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Errorf("symlink escaping config root = %v, want no paths", paths)
	}
}

func TestCoverageSourcePathsRespectFilesystemCaseBehavior(t *testing.T) {
	root := t.TempDir()
	realFile := filepath.Join(root, "src", "a.c")
	if err := os.MkdirAll(filepath.Dir(realFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	caseVariant := func(path string) string {
		for i, r := range path {
			if r >= 'a' && r <= 'z' {
				return path[:i] + strings.ToUpper(string(r)) + path[i+1:]
			}
			if r >= 'A' && r <= 'Z' {
				return path[:i] + strings.ToLower(string(r)) + path[i+1:]
			}
		}
		return path
	}

	rootAlias := filepath.Join(filepath.Dir(root), caseVariant(filepath.Base(root)))
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	aliasInfo, aliasErr := os.Stat(rootAlias)
	rootCaseAliases := aliasErr == nil && os.SameFile(rootInfo, aliasInfo)
	paths, err := coverageSourcePaths(rootAlias, nil, realFile)
	if err != nil {
		t.Fatal(err)
	}
	if rootCaseAliases {
		if len(paths) != 1 || paths[0] != canonicalPath(realFile) {
			t.Errorf("case-aliased root paths = %v, want [%s]", paths, canonicalPath(realFile))
		}
	} else if len(paths) != 0 {
		t.Errorf("case-sensitive root alias paths = %v, want none", paths)
	}

	fileAlias := caseVariant(realFile)
	realInfo, err := os.Stat(realFile)
	if err != nil {
		t.Fatal(err)
	}
	aliasFileInfo, fileAliasErr := os.Stat(fileAlias)
	fileCaseAliases := fileAliasErr == nil && os.SameFile(realInfo, aliasFileInfo)
	paths, err = coverageSourcePaths(root, nil, fileAlias)
	if err != nil {
		t.Fatal(err)
	}
	if fileCaseAliases {
		if len(paths) != 1 || paths[0] != canonicalPath(realFile) {
			t.Errorf("case-aliased file paths = %v, want [%s]", paths, canonicalPath(realFile))
		}
	} else if len(paths) != 0 {
		t.Errorf("case-sensitive file alias paths = %v, want none", paths)
	}
}

func TestCoverageSourcePathsResolveUnixBackslashAliasesWithoutLosingLiteralNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("backslash is a path separator on Windows")
	}
	root := t.TempDir()
	normal := filepath.Join(root, "src", "a.c")
	if err := os.MkdirAll(filepath.Dir(normal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(normal, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	backslashPath := filepath.Join(root, `src\a.c`)
	paths, err := coverageSourcePaths(root, nil, `src\a.c`)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != canonicalPath(normal) {
		t.Errorf("slash alias paths = %v, want normalized [%s]", paths, canonicalPath(normal))
	}

	if err := os.WriteFile(backslashPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := coverageSourcePaths(root, nil, `src\a.c`); err == nil || !strings.Contains(err.Error(), "ambiguous backslash path") {
		t.Fatalf("both literal and separator paths exist; error = %v, want ambiguity", err)
	}

	if err := os.Remove(normal); err != nil {
		t.Fatal(err)
	}
	paths, err = coverageSourcePaths(root, nil, `src\a.c`)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != canonicalPath(backslashPath) {
		t.Errorf("literal backslash filename paths = %v, want [%s]", paths, canonicalPath(backslashPath))
	}
}

func TestCoverageSourcePathsIgnoreForeignPlatformAbsolutePaths(t *testing.T) {
	cases := []struct {
		path, goos string
		foreign    bool
	}{
		{`C:\agent\repo\src\a.c`, "linux", true},
		{`C:\agent\repo\src\a.c`, "windows", false},
		{`\repo\src\a.c`, "linux", true},
		{`C:repo\src\a.c`, "linux", true},
		{`\\server\share\repo\src\a.c`, "linux", true},
		{`\\server\share\repo\src\a.c`, "windows", false},
		{`/repo/src/a.c`, "linux", false},
		{`/repo/src/a.c`, "windows", true},
		{`//server/share/repo/src/a.c`, "linux", false},
		{`//server/share/repo/src/a.c`, "windows", false},
		{`\repo\src\a.c`, "windows", true},
		{`C:repo\src\a.c`, "windows", true},
		{`src/a.c`, "linux", false},
		{`src/a.c`, "windows", false},
	}
	for _, tc := range cases {
		if got := isForeignAbsoluteCoveragePathForOS(tc.path, tc.goos); got != tc.foreign {
			t.Errorf("isForeignAbsoluteCoveragePathForOS(%q, %q) = %v, want %v", tc.path, tc.goos, got, tc.foreign)
		}
	}
}

func TestExistingCoverageCandidatesTreatsENOTDIRAsMissing(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "go.mod")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	existing, err := existingCoverageCandidates([]string{filepath.Join(file, "a.go")})
	if err != nil {
		t.Fatalf("ENOTDIR candidate should be treated as missing: %v", err)
	}
	if len(existing) != 0 {
		t.Fatalf("existing candidates = %v, want none", existing)
	}
}

func TestCoveragePathResolverCachesCaseIdentityLookups(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "SourceDir")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "sourcedir")
	realInfo, err := os.Stat(realDir)
	if err != nil {
		t.Fatal(err)
	}
	aliasInfo, err := os.Stat(alias)
	if err != nil || !os.SameFile(realInfo, aliasInfo) {
		t.Skip("temporary volume is case-sensitive")
	}
	reads := map[string]int{}
	resolver := newCoveragePathResolver()
	resolver.readDir = func(path string) ([]os.DirEntry, error) {
		reads[path]++
		return os.ReadDir(path)
	}
	for range 2 {
		got, err := canonicalCoveragePathWithResolver(alias, resolver)
		if err != nil {
			t.Fatal(err)
		}
		if got != realDir {
			t.Fatalf("canonical alias = %q, want on-disk path %q", got, realDir)
		}
	}
	if reads[root] != 1 {
		t.Fatalf("case identity directory reads = %d, want one cached read", reads[root])
	}
}

func TestCoveragePathStatErrorsDoNotHideCandidates(t *testing.T) {
	root := t.TempDir()
	normal := filepath.Join(root, "src", "a.go")
	if err := os.MkdirAll(filepath.Dir(normal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(normal, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		literal := filepath.Join(root, `src\a.go`)
		resolver := newCoveragePathResolver()
		resolver.stat = func(path string) (os.FileInfo, error) {
			if path == literal {
				return nil, &os.PathError{Op: "stat", Path: path, Err: syscall.EACCES}
			}
			return os.Stat(path)
		}
		if _, err := coverageSourcePathsWithResolver(root, nil, `src\a.go`, resolver); err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("backslash candidate permission error = %v, want propagated error", err)
		}
	}
	resolver := newCoveragePathResolver()
	resolver.stat = func(path string) (os.FileInfo, error) {
		return nil, &os.PathError{Op: "stat", Path: path, Err: syscall.EACCES}
	}
	_, err := existingCoverageCandidatesWithResolver([]string{normal}, resolver)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("candidate permission error = %v, want propagated error", err)
	}
}

func TestBackslashCandidatesAreContainedBeforeStat(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Clean(filepath.Join(root, "../../outside/a.go"))
	resolver := newCoveragePathResolver()
	calledOutside := false
	resolver.stat = func(path string) (os.FileInfo, error) {
		if filepath.Clean(path) == outside {
			calledOutside = true
			return nil, &os.PathError{Op: "stat", Path: path, Err: syscall.EACCES}
		}
		return os.Stat(path)
	}
	paths, err := coverageSourcePathsWithResolver(root, nil, `..\..\outside\a.go`, resolver)
	if err != nil {
		t.Fatalf("outside slash candidate should be ignored: %v", err)
	}
	if calledOutside {
		t.Fatal("stat was called for an out-of-root slash candidate")
	}
	if len(paths) != 1 {
		t.Fatalf("literal in-root filename evidence = %v, want one path", paths)
	}
}

func TestBackslashLiteralENAMETOOLONGIsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("backslash is a path separator on Windows")
	}
	root := t.TempDir()
	source := strings.Repeat("x", 300) + `\a.go`
	paths, err := coverageSourcePaths(root, nil, source)
	if err != nil {
		t.Fatalf("overlong literal filename should count as missing: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("missing in-root candidate = %v, want one path", paths)
	}
}

func TestBackslashAlternateSymlinkEscapeDoesNotMakeLiteralAmbiguous(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("backslash is a path separator on Windows")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(parent, "external")
	if err := os.Mkdir(external, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "a.c"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "src")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	literal := filepath.Join(root, `src\a.c`)
	if err := os.WriteFile(literal, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := coverageSourcePaths(root, nil, `src\a.c`)
	if err != nil {
		t.Fatalf("out-of-root slash candidate should be ignored: %v", err)
	}
	want := canonicalPath(literal)
	if len(paths) != 1 || paths[0] != want {
		t.Fatalf("resolved paths = %v, want only literal in-root file %q", paths, want)
	}
}

func TestCoverageSourcePathsDeduplicateHardlinksAndUnicodeAliases(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "a.go")
	second := filepath.Join(root, "b.go")
	if err := os.WriteFile(first, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(first, second); err != nil {
		t.Skipf("hard link unavailable: %v", err)
	}
	resolver := newCoveragePathResolver()
	pathsA, err := coverageSourcePathsWithResolver(root, nil, "a.go", resolver)
	if err != nil {
		t.Fatal(err)
	}
	pathsB, err := coverageSourcePathsWithResolver(root, nil, "b.go", resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(pathsA) != 1 || len(pathsB) != 1 || pathsA[0] != pathsB[0] {
		t.Errorf("hardlink paths = %v and %v, want one shared identity", pathsA, pathsB)
	}
	data := []byte("SF:a.go\nLF:10\nLH:5\nend_of_record\nSF:b.go\nLF:10\nLH:5\nend_of_record\n")
	_, _, _, err = lcovCoverage(data, "lines", root, true, true)
	if err == nil || !strings.Contains(err.Error(), "duplicate source evidence") {
		t.Errorf("LCOV hardlink duplicate error = %v, want duplicate evidence failure", err)
	}

	nfc := "café.go"
	nfd := strings.Replace(nfc, "é", "e\u0301", 1)
	nfcPath := filepath.Join(root, nfc)
	nfdPath := filepath.Join(root, nfd)
	if err := os.WriteFile(nfcPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	infoNFC, err := os.Stat(nfcPath)
	if err != nil {
		t.Fatal(err)
	}
	infoNFD, nfdErr := os.Stat(nfdPath)
	if nfdErr == nil && os.SameFile(infoNFC, infoNFD) {
		pathsNFC, err := coverageSourcePathsWithResolver(root, nil, nfc, resolver)
		if err != nil {
			t.Fatal(err)
		}
		pathsNFD, err := coverageSourcePathsWithResolver(root, nil, nfd, resolver)
		if err != nil {
			t.Fatal(err)
		}
		if len(pathsNFC) != 1 || len(pathsNFD) != 1 || pathsNFC[0] != pathsNFD[0] {
			t.Errorf("Unicode alias paths = %v and %v, want one shared identity", pathsNFC, pathsNFD)
		}
	}
}

func TestCoverageSourceResolutionPropagatesCanonicalizationErrors(t *testing.T) {
	root := t.TempDir()
	resolver := newCoveragePathResolver()
	resolver.readDir = func(path string) ([]os.DirEntry, error) {
		return nil, &os.PathError{Op: "readdir", Path: path, Err: syscall.EACCES}
	}
	if _, err := coverageSourcePathsWithResolver(root, nil, "a.go", resolver); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("ReadDir permission error = %v, want propagated measurement error", err)
	}

	resolver = newCoveragePathResolver()
	resolver.stat = func(path string) (os.FileInfo, error) {
		return nil, &os.PathError{Op: "stat", Path: path, Err: syscall.EACCES}
	}
	if _, err := coverageSourcePathsWithResolver(root, nil, "a.go", resolver); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("canonical Stat permission error = %v, want propagated measurement error", err)
	}
}

func TestCoverageMinFoundZeroRequiresValidSourceEvidence(t *testing.T) {
	dir := t.TempDir()
	data := []byte("SF:/outside/a.go\nLF:10\nLH:5\nend_of_record\n")
	_, _, _, err := lcovCoverage(data, "lines", dir, false, true)
	if err == nil || !strings.Contains(err.Error(), "no valid in-directory SF source evidence") {
		t.Fatalf("readLCOVCoverage without valid source evidence error = %v, want clear evidence error", err)
	}
}
