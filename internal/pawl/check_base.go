package pawl

import (
	"fmt"
	"path/filepath"
	"strings"
)

func readSnapshotAtRef(cfg *Config, ref string) (*Snapshot, error) {
	root, code, gitErr := gitOutput(cfg.Dir, "rev-parse", "--show-toplevel")
	if code != 0 {
		return nil, fmt.Errorf("check --base: %s is not inside a git repository: %s", cfg.Dir, gitErr)
	}
	commit, code, gitErr := gitOutput(cfg.Dir, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if code != 0 {
		return nil, fmt.Errorf("check --base: could not resolve git ref %q: %s", ref, gitErr)
	}
	relPath, err := filepath.Rel(root, cfg.SnapshotPath)
	if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("check --base: snapshot %s is outside the git repository %s", cfg.SnapshotPath, root)
	}
	relPath = filepath.ToSlash(relPath)
	data, code, gitErr := gitOutput(cfg.Dir, "show", commit+":"+relPath)
	if code != 0 {
		return nil, fmt.Errorf("check --base: no snapshot %s at %s: %s", relPath, ref, gitErr)
	}
	snapshot, parsed, err := ParseSnapshot([]byte(data))
	if err != nil {
		return nil, fmt.Errorf("check --base: snapshot %s at %s is %v", relPath, ref, err)
	}
	if shapeErrors := SnapshotShapeErrors(parsed); len(shapeErrors) > 0 {
		return nil, fmt.Errorf("check --base: snapshot %s at %s has invalid shape: %s", relPath, ref, strings.Join(shapeErrors, "; "))
	}
	return snapshot, nil
}
