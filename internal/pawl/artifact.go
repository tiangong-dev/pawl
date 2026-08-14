package pawl

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ArtifactInfo describes the file one measurement read off disk.
//
// A dimension configured with `file:` and no `command:` reads whatever is
// there. A JUnit report written days ago parses exactly like one written a
// second ago and produces a number the verdict presents like any other — the
// gate is then measuring the past. pawl cannot tell from the bytes whether the
// artifact matches the working tree, so it reports what it does know: which
// file, when it was last written, and whether this invocation produced it.
// This is provenance, not a gate — it never changes an exit code.
type ArtifactInfo struct {
	Path       string    `json:"path"`
	Modified   time.Time `json:"modified"`
	AgeSeconds int64     `json:"age_seconds"`
	// Generated is true when the dimension's own command wrote the file during
	// this invocation (the reader deletes it first), which makes the age
	// meaningless-by-construction rather than informative.
	Generated bool `json:"generated"`
}

// statArtifact describes a file a measurement has just read. A stat failure
// returns nil rather than an error: the bytes were already read successfully,
// so failing the measurement over missing provenance would turn a working gate
// into a broken one.
func statArtifact(cfg *Config, fileRel string, generated bool) *ArtifactInfo {
	info, err := os.Stat(filepath.Join(cfg.Dir, fileRel))
	if err != nil {
		return nil
	}
	mod := info.ModTime()
	return &ArtifactInfo{
		Path:       fileRel,
		Modified:   mod,
		AgeSeconds: int64(time.Since(mod).Seconds()),
		Generated:  generated,
	}
}

// formatArtifactAge renders an age at one significant unit — enough to answer
// "is this from this run or from last week". A negative age (an artifact
// stamped in the future by a clock skew or an archive extraction) keeps its
// sign rather than being clamped to zero: the oddity is the signal.
func formatArtifactAge(seconds int64) string {
	sign := ""
	if seconds < 0 {
		sign, seconds = "-", -seconds
	}
	switch {
	case seconds < 60:
		return fmt.Sprintf("%s%ds", sign, seconds)
	case seconds < 3600:
		return fmt.Sprintf("%s%dm", sign, seconds/60)
	case seconds < 86400:
		return fmt.Sprintf("%s%dh", sign, seconds/3600)
	default:
		return fmt.Sprintf("%s%dd", sign, seconds/86400)
	}
}
