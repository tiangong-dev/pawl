package pawl

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// defaultConfigName is the file a config-reading command looks for when no
// -c/--config was given.
const defaultConfigName = "pawl.yaml"

// resolveConfigPath returns the config a command must read. An explicit -c is
// used verbatim — a typo stays a loud "not found" rather than silently adopting
// another file. Without one, pawl takes the nearest pawl.yaml from the working
// directory upward, the way git and eslint resolve their own files, so
// `pawl check` inside a subdirectory stops demanding its own -c. The walk stops
// at the nearest ancestor holding a .git entry (a directory, or a file as in a
// git worktree), that directory included; outside a git repository only the
// working directory is searched, so an unrelated $HOME/pawl.yaml is never
// adopted by an unversioned tree. A config adopted from a parent is announced
// on progress (stderr, silenced by -q), never on stdout.
//
// A config in the working directory is returned under the plain relative name
// `pawl.yaml`, so every message that quotes the config path reads exactly as it
// did before discovery existed.
func resolveConfigPath(explicit string, explicitGiven bool, progress io.Writer) (string, error) {
	if explicitGiven {
		return explicit, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolving the working directory: %w", err)
	}
	// The search ends at the enclosing repository root, which may be the working
	// directory itself; outside a repository it is the working directory alone.
	bound, inRepo := gitRoot(cwd)
	if !inRepo {
		bound = cwd
	}
	dir := cwd
	for {
		candidate := filepath.Join(dir, defaultConfigName)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			if dir == cwd {
				return defaultConfigName, nil
			}
			fmt.Fprintf(progress, "pawl: using config %s\n", candidate)
			return candidate, nil
		}
		if dir == bound {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	switch {
	case !inRepo:
		return "", fmt.Errorf("no %s found in %s (searched only this directory: no .git in any parent to bound a wider search)", defaultConfigName, cwd)
	case bound == cwd:
		return "", fmt.Errorf("no %s found in %s (repository root)", defaultConfigName, cwd)
	default:
		return "", fmt.Errorf("no %s found in %s or any parent up to %s", defaultConfigName, cwd, bound)
	}
}

// gitRoot returns the nearest ancestor of start (inclusive) that holds a .git
// entry — a directory in a normal clone, a file in a git worktree. found is
// false when no ancestor is a repository, which is what distinguishes a working
// directory that is itself the repository root from one outside any repository.
func gitRoot(start string) (root string, found bool) {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
