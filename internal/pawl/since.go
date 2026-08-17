package pawl

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Diff-scoped checking. `check --since <ref>` runs the normal gate, then
// scopes the verdict to lines changed since <ref>: pre-existing debt on
// unchanged lines is exempted, new regressions on added lines still fail. It is
// the gate narrowed to new code, not a standalone scanner. See
// spec/commands/since.md § Diff-scoped checking.

// sinceScope is the resolved changed-line context for one --since run.
type sinceScope struct {
	ref          string
	mergeBase    string
	added        map[string]map[int]bool // repo-relative path → added new-line numbers
	relDir       string                  // config dir relative to the repo toplevel
	exempted     int                     // regressions suppressed as pre-existing
	unscopeable  int                     // live regressions with no attributable line
	enforcedFull []string                // dimension ids kept at full strength (no line to scope)
}

// applySinceScope resolves the changed lines since ref and rewrites rep in
// place. Any git failure — an unresolvable ref, missing merge-base, or a
// config dir outside the repo — is returned so the caller can abort as
// could-not-measure rather than a silent "nothing changed".
func applySinceScope(cfg *Config, rep *Report, baseline *Snapshot, current map[string]Metric, ref string) (*sinceScope, error) {
	added, mergeBase, err := addedLinesSince(cfg.Dir, ref)
	if err != nil {
		return nil, fmt.Errorf("check --since: %w", err)
	}
	relDir, err := gitRelDir(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("check --since: %w", err)
	}

	scope := &sinceScope{ref: ref, mergeBase: mergeBase, added: added, relDir: relDir}
	rep.Mode = "since"
	rep.Since = &ref
	dimByID := map[string]Dimension{}
	for _, d := range cfg.Dimensions {
		dimByID[d.ID] = d
	}
	for i := range rep.Metrics {
		m := &rep.Metrics[i]
		scope.scopeMetric(m, dimByID[m.ID], baseline.Metrics[m.ID], current[m.ID])
	}
	return scope, nil
}

// scopeMetric rebuilds one metric's regressions for --since. A total-gate
// dimension (and a line-addressable one with no breakdown to attribute) is
// enforced in full. Otherwise the verdict is rebuilt from the breakdowns: an
// offender on an added line is a live regression, one on an unchanged line is
// suppressed, and one with no attributable line is kept live (conservative).
func (s *sinceScope) scopeMetric(m *MetricReport, dim Dimension, base, cur Metric) {
	if m.Base == nil {
		return // a new dimension has no baseline to regress against
	}
	gate := dim.Gate
	if gate == "" {
		gate = GateTotal
	}
	// Only per-file-count is cleanly line-attributable: its scalar is the count /
	// sum of a `path:line` breakdown, so every contributor to a regression has a
	// line. `total` (no breakdown) and `per-key-value` (a scalar that is not a sum
	// of its breakdown, and which ignores new keys) cannot be scoped without
	// diverging from the full-mode verdict — inventing a regression full mode
	// wouldn't raise, or dropping one it would — so they are enforced at full
	// strength: the full-mode verdict stands, unscoped. A per-file-count dimension
	// with no breakdown to attribute is treated the same way.
	if gate != GatePerFileCount || len(cur.Breakdown) == 0 ||
		!sameFindingTotal(base.Value, base.Breakdown) || !sameFindingTotal(cur.Value, cur.Breakdown) {
		if len(m.Regressions) > 0 {
			s.enforcedFull = append(s.enforcedFull, m.ID)
		}
		return
	}

	spec := dim.GateSpecOf()
	out := []Regression{}
	live := 0

	// The scalar is listed for faithfulness (a consumer must still see it moved),
	// but suppressed: the per-key pass below accounts for every contributor to a
	// per-file-count total, so counting the scalar too would double-gate it.
	if Worse(spec.Direction, base.Value, cur.Value, spec.Tolerance) {
		out = append(out, Regression{
			Kind:       "total",
			Base:       base.Value,
			Current:    cur.Value,
			Message:    fmt.Sprintf("total %s → %s", FormatNumber(base.Value), FormatNumber(cur.Value)),
			Suppressed: true,
		})
	}

	// A per-file-count key is worse when an offender appeared (a new key) or an
	// edited line gained offenders (its value grew) — direction-agnostic, exactly
	// what moves the full-mode count/scalar. Each worse key is live if its line
	// was changed, suppressed if not, and (conservatively) live if unscopeable.
	for _, fileRegression := range perFileRegressions(base.Breakdown, cur.Breakdown) {
		for _, key := range fileRegression.Contributors {
			baseVal := base.Breakdown[key]
			reg := keyRegression(string(gate), key, baseVal, cur.Breakdown[key],
				fmt.Sprintf("%s  %s → %s", key, FormatNumber(baseVal), FormatNumber(cur.Breakdown[key])))
			switch {
			case reg.Line == nil || *reg.Line <= 0:
				s.unscopeable++ // no attributable line → cannot prove pre-existing, gate it
				live++
			case s.added[s.repoPath(*reg.Path)][*reg.Line]:
				live++ // an offender on a changed line
			default:
				reg.Suppressed = true
				s.exempted++
			}
			out = append(out, reg)
		}
	}

	m.Regressions = out
	// Status reflects the full-mode reality, recomputed so it is not forced to
	// "worse" merely because suppressed entries are listed. It may still read
	// "worse" with exit 0 when the scalar genuinely worsened but --since exempted
	// it as pre-existing — the suppressed entries and exit code carry the verdict.
	if live > 0 {
		m.Status = "worse"
	} else {
		m.Status = statusName(spec.Direction, m.Base, *m.Current, spec.Tolerance)
	}
}

func sameFindingTotal(value float64, breakdown map[string]float64) bool {
	sum := 0.0
	for _, count := range breakdown {
		sum += count
	}
	return math.Abs(value-sum) <= 1e-9
}

// repoPath maps a config-relative breakdown path to the repo-relative form the
// git-diff added-line index is keyed by.
func (s *sinceScope) repoPath(p string) string {
	if s.relDir == "." || s.relDir == "" {
		return p
	}
	return s.relDir + "/" + p
}

// addedLinesSince returns the added (new-file) lines between merge-base(ref,HEAD)
// and the working tree, keyed by repo-relative path, plus the merge-base commit.
// The working tree (tracked edits + untracked files) is the comparison, not
// HEAD: an agent that check --since before committing would otherwise see an
// empty added-line set and have new debt suppressed as "pre-existing". In CI
// the working tree matches HEAD, so the committed-only form is unchanged.
// Ref resolution and merge-base are separate git calls so a typo'd ref fails
// loud rather than silently disabling the scoping.
func addedLinesSince(dir, ref string) (map[string]map[int]bool, string, error) {
	if _, code, gitErr := gitOutput(dir, "rev-parse", "--verify", ref); code != 0 {
		return nil, "", fmt.Errorf("could not resolve ref %q: %s", ref, gitErr)
	}
	mergeBase, code, gitErr := gitOutput(dir, "merge-base", ref, "HEAD")
	if code != 0 {
		return nil, "", fmt.Errorf("no merge-base between %q and HEAD: %s", ref, gitErr)
	}
	// Working tree vs merge-base (staged + unstaged). Untracked files are not
	// in this diff; they are unioned in below.
	//
	// The header prefixes are pinned: diff.mnemonicPrefix (which renames a//b/
	// to c//w/), diff.noprefix, and diff.srcPrefix/dstPrefix are all developer
	// preferences that would make every `+++ b/<path>` header parse to the wrong
	// path — an empty added-line set, which reads as "nothing changed" and
	// exempts every new offender as pre-existing debt.
	diff, code, gitErr := gitOutput(dir, "diff", "--unified=0", "--no-ext-diff",
		"--src-prefix=a/", "--dst-prefix=b/", mergeBase)
	if code != 0 {
		return nil, "", fmt.Errorf("git diff failed: %s", gitErr)
	}
	added := parseAddedLines(diff)
	if err := addUntrackedLines(dir, added); err != nil {
		return nil, "", err
	}
	return added, mergeBase, nil
}

// addUntrackedLines unions every line of every untracked, non-ignored file
// into the added-line set. A brand-new file the agent has not `git add`ed
// yet is the common local-loop case; treating it as all-added keeps --since
// from suppressing its findings as pre-existing debt.
func addUntrackedLines(dir string, added map[string]map[int]bool) error {
	listed, code, gitErr := gitOutputRaw(dir, "ls-files", "--others", "--exclude-standard", "--full-name", "-z")
	if code != 0 {
		return fmt.Errorf("git ls-files failed: %s", gitErr)
	}
	if listed == "" {
		return nil
	}
	toplevel, code, gitErr := gitOutput(dir, "rev-parse", "--show-toplevel")
	if code != 0 {
		return fmt.Errorf("git ls-files: %s is not inside a git repository: %s", dir, gitErr)
	}
	toplevel = canonicalPath(toplevel)
	for _, rel := range strings.Split(listed, "\x00") {
		if rel == "" {
			continue
		}
		path := filepath.Join(toplevel, rel)
		// Only a readable regular file can carry an offender: measurement skips
		// everything else (walkIncluded), so a dangling symlink, a socket, or a
		// file whose permissions deny reading cannot hide one from the scope.
		// Failing on them instead would let an ordinary working tree turn the
		// gate into "could not measure".
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		n := lineCount(data)
		if n == 0 {
			continue
		}
		if added[rel] == nil {
			added[rel] = map[int]bool{}
		}
		for i := 1; i <= n; i++ {
			added[rel][i] = true
		}
	}
	return nil
}

// parseAddedLines parses a `git diff --unified=0` into added new-file line
// numbers per repo-relative path. It tracks hunk state so a hunk-body content
// line that happens to start with `+++ ` is recorded as an added line, not
// mistaken for a file header (headers appear only before the first `@@`).
func parseAddedLines(diff string) map[string]map[int]bool {
	added := map[string]map[int]bool{}
	curPath := ""
	newLine := 0
	inHunk := false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			inHunk = false
			curPath = ""
		case !inHunk && strings.HasPrefix(line, "+++ "):
			curPath = parseDiffPath(line[len("+++ "):])
		case !inHunk && strings.HasPrefix(line, "--- "):
			// old-file header, before the hunks; ignored.
		case strings.HasPrefix(line, "@@"):
			inHunk = true
			newLine = parseHunkNewStart(line)
		case inHunk && strings.HasPrefix(line, "+"):
			if curPath != "" && newLine > 0 {
				if added[curPath] == nil {
					added[curPath] = map[int]bool{}
				}
				added[curPath][newLine] = true
			}
			newLine++
		case inHunk && strings.HasPrefix(line, "-"):
			// deletion: old-side only, does not advance the new-line counter.
		case inHunk && strings.HasPrefix(line, "\\"):
			// `\ No newline at end of file` annotates the line before it and is
			// not itself a line of the file. git emits it between the `-` and
			// `+` halves of the hunk, so counting it would push every following
			// added line one down and drop the real one out of the scope.
		case inHunk:
			newLine++ // a context line (rare at unified=0) advances the new side.
		}
	}
	return added
}

// parseDiffPath extracts the repo-relative path from a `+++ ` header: it strips
// the `b/` prefix, unquotes a git-quoted path, drops a trailing tab-timestamp,
// and maps /dev/null (a deletion) to the empty path.
func parseDiffPath(s string) string {
	s = strings.TrimRight(s, "\r\n")
	if i := strings.IndexByte(s, '\t'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if s == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(s, "\"") {
		if unquoted, err := strconv.Unquote(s); err == nil {
			s = unquoted
		}
	}
	s = strings.TrimPrefix(s, "b/")
	s = strings.TrimPrefix(s, "a/")
	return s
}

// parseHunkNewStart reads the new-file start line from an `@@ -a,b +c,d @@`
// header (the number after `+`).
func parseHunkNewStart(line string) int {
	plus := strings.IndexByte(line, '+')
	if plus < 0 {
		return 0
	}
	rest := line[plus+1:]
	if end := strings.IndexAny(rest, ", "); end >= 0 {
		rest = rest[:end]
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0
	}
	return n
}

// renderSinceText prints the diff-mode banner and the scoped verdict: which
// dimensions were enforced in full, the live regressions on changed lines, and
// how many pre-existing regressions were exempted.
func renderSinceText(w io.Writer, rep *Report, scope *sinceScope) {
	short := scope.mergeBase
	if len(short) > 7 {
		short = short[:7]
	}
	fmt.Fprintf(w, "\ndiff mode — since %s (merge-base %s)\n", scope.ref, short)
	if len(scope.enforcedFull) > 0 {
		full := append([]string(nil), scope.enforcedFull...)
		sort.Strings(full)
		fmt.Fprintf(w, "  enforced in full (no line to scope): %s\n", strings.Join(full, ", "))
	}
	var live []MetricReport
	for _, m := range rep.Metrics {
		for _, r := range m.Regressions {
			if !r.Suppressed {
				live = append(live, m)
				break
			}
		}
	}
	if len(live) > 0 {
		fmt.Fprintln(w, "❌ regressions on changed lines:")
		for _, m := range live {
			fmt.Fprintf(w, "  • %s (%s)\n", m.ID, m.Title)
			for _, r := range m.Regressions {
				if !r.Suppressed {
					fmt.Fprintf(w, "      %s\n", r.Message)
				}
			}
		}
	}
	if scope.exempted > 0 {
		fmt.Fprintf(w, "%d pre-existing regression(s) exempted by --since\n", scope.exempted)
	}
	if scope.unscopeable > 0 {
		fmt.Fprintf(w, "⚠️  %d regression(s) counted with no attributable line (unscopeable)\n", scope.unscopeable)
	}
	if len(live) == 0 {
		fmt.Fprintln(w, "✅ no regressions on changed lines.")
	}
}
