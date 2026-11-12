package pawl

import "strings"

// Git rename detection for guard's per-file comparison. A pure rename moves
// existing offenders to a new path, which a name-only comparison reads as a
// brand-new file full of debt — the false positive this mapping removes. It
// cannot make guard more lenient: only base counts git attributes to the same
// file are re-keyed, and any pair that is not 1:1 or whose destination the base
// already knows is dropped, which leaves the plain per-path comparison in
// place.

// parseRenameRecords splits the NUL-separated records of
// `git diff --name-status -z --diff-filter=R` into old→new path pairs. Each
// rename is three records — the "R<score>" status, the source path, the
// destination path — so an incomplete tail, or a record that is not a rename,
// stops the parse rather than letting the paths that follow shift into the
// wrong roles.
func parseRenameRecords(out string) [][2]string {
	fields := strings.Split(out, "\x00")
	var pairs [][2]string
	for i := 0; i+2 < len(fields); i += 3 {
		status, from, to := fields[i], fields[i+1], fields[i+2]
		if !strings.HasPrefix(status, "R") || from == "" || to == "" {
			break
		}
		pairs = append(pairs, [2]string{from, to})
	}
	return pairs
}

// guardRenames asks git for the renames between merge-base(ref, HEAD) and HEAD
// and returns them as config-relative old→new paths, the form a metric's
// breakdown keys use. ok is false when the question cannot be asked at all —
// the ref has no merge base with HEAD (a shallow clone), the diff fails, or the
// config dir is outside the repository — and the caller then compares without
// mapping: unavailable detection is stricter, never more lenient. A rename with
// an end outside the config dir is dropped, because no breakdown key can name
// it.
//
// --find-renames is pinned so the user's diff.renames setting cannot switch
// detection off. Copies are deliberately left alone: an added copy is new code
// that happens to look like existing code, not a move of it.
func guardRenames(dir, ref string) (map[string]string, bool) {
	mergeBase, code, _ := gitOutput(dir, "merge-base", ref, "HEAD")
	if code != 0 {
		return nil, false
	}
	out, code, _ := gitOutputRaw(dir, "diff", "--find-renames", "--name-status", "-z",
		"--diff-filter=R", "--no-ext-diff", mergeBase+"..HEAD")
	if code != 0 {
		return nil, false
	}
	relDir, err := gitRelDir(dir)
	if err != nil {
		return nil, false
	}
	renames := map[string]string{}
	for _, pair := range parseRenameRecords(out) {
		from, okFrom := repoPathToConfig(pair[0], relDir)
		to, okTo := repoPathToConfig(pair[1], relDir)
		if !okFrom || !okTo || from == to {
			continue
		}
		renames[from] = to
	}
	return renames, true
}
