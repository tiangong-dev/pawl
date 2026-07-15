package pawl

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// ccEntry is one Code Climate issue — the subset GitLab's Code Quality widget
// requires. Emitted by `--format codeclimate`.
type ccEntry struct {
	Description string     `json:"description"`
	CheckName   string     `json:"check_name"`
	Fingerprint string     `json:"fingerprint"`
	Severity    string     `json:"severity"`
	Location    ccLocation `json:"location"`
}

type ccLocation struct {
	Path  string  `json:"path"`
	Lines ccLines `json:"lines"`
}

type ccLines struct {
	Begin int `json:"begin"`
}

// buildCodeClimate turns the current measurement into Code Climate findings —
// one entry per per-file-count offender that resolves to a path:line. This is
// findings mode: it reports every current offender independent of the snapshot,
// leaving the new-vs-fixed delta to GitLab (which diffs the MR-branch report
// against the target-branch one). total and per-key-value dimensions carry no
// per-line location, so they contribute nothing.
func buildCodeClimate(cfg *Config, current map[string]Metric) []ccEntry {
	entries := []ccEntry{}
	for _, dim := range cfg.Dimensions {
		if dim.Gate != GatePerFileCount {
			continue
		}
		for key, count := range current[dim.ID].Breakdown {
			path, lineStr, hasLine := strings.Cut(key, ":")
			if !hasLine || lineStr == "" {
				continue // Code Quality entries require a line; a bare path has none.
			}
			line, err := strconv.Atoi(lineStr)
			if err != nil {
				continue
			}
			desc := dim.Title
			if count > 1 {
				desc = fmt.Sprintf("%s ×%s", desc, FormatNumber(count))
			}
			entries = append(entries, ccEntry{
				Description: desc,
				CheckName:   dim.ID,
				Fingerprint: ccFingerprint(dim.ID, path, line, desc),
				Severity:    "major",
				Location:    ccLocation{Path: path, Lines: ccLines{Begin: line}},
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Location.Path != b.Location.Path {
			return a.Location.Path < b.Location.Path
		}
		if a.Location.Lines.Begin != b.Location.Lines.Begin {
			return a.Location.Lines.Begin < b.Location.Lines.Begin
		}
		return a.CheckName < b.CheckName
	})
	return entries
}

// ccFingerprint is a stable digest identifying one issue across commits so
// GitLab tracks the same offender rather than re-flagging a re-measured one as
// new. Fields are NUL-separated so distinct boundaries can't collide.
func ccFingerprint(checkName, path string, line int, description string) string {
	sum := md5.Sum([]byte(checkName + "\x00" + path + "\x00" + strconv.Itoa(line) + "\x00" + description))
	return hex.EncodeToString(sum[:])
}

// renderCodeClimate writes the findings as a Code Climate array plus a trailing
// newline — stdout stays pure JSON (no table, no emoji, no annotations). An
// empty set of findings prints `[]`.
func renderCodeClimate(w io.Writer, cfg *Config, current map[string]Metric) error {
	data, err := json.MarshalIndent(buildCodeClimate(cfg, current), "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}
