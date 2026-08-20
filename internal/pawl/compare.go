package pawl

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Worse reports whether cur regressed past base by more than tolerance —
// absolute slack in the worse direction. Exactly AT the tolerance boundary
// passes.
func Worse(d Direction, base, cur, tolerance float64) bool {
	if d == HigherIsBetter {
		return cur < base-tolerance
	}
	return cur > base+tolerance
}

// Better reports strict improvement (tolerance never applies).
func Better(d Direction, base, cur float64) bool {
	if d == HigherIsBetter {
		return cur > base
	}
	return cur < base
}

// FormatNumber prints v in minimal decimal notation, never exponent form —
// snapshot values and regression lines must stay grep-able and diff-stable
// at any magnitude.
func FormatNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// OffenderCountsByFile sums finding multiplicity grouped by file. A breakdown
// value is the number of findings at that path:line, so counting only map keys
// would collapse multiple rules or regexp matches on one line into one
// offender. Summing remains robust to findings moving within a file: only the
// file total matters.
func OffenderCountsByFile(breakdown map[string]float64) map[string]float64 {
	out := map[string]float64{}
	for k, count := range breakdown {
		file, _ := splitLocationKey(k)
		out[file] += count
	}
	return out
}

type perFileRegression struct {
	File         string
	Base         float64
	Current      float64
	Contributors []string
}

// perFileRegressions is the single source of truth for per-file-count
// comparison. A file regresses when its summed finding count rises. The
// contributors are the new or increased path:line keys that explain that
// aggregate rise; renderers and --since consume the same list.
func perFileRegressions(base, cur map[string]float64) []perFileRegression {
	bFiles := OffenderCountsByFile(base)
	cFiles := OffenderCountsByFile(cur)
	contributors := map[string][]string{}
	for _, key := range sortedKeys(cur) {
		file, _ := splitLocationKey(key)
		if cur[key] > base[key] {
			contributors[file] = append(contributors[file], key)
		}
	}
	var out []perFileRegression
	for _, file := range sortedKeyUnion(bFiles, cFiles) {
		if cFiles[file] <= bFiles[file] {
			continue
		}
		out = append(out, perFileRegression{
			File:         file,
			Base:         bFiles[file],
			Current:      cFiles[file],
			Contributors: contributors[file],
		})
	}
	return out
}

// RegressionsOf returns human-readable regression lines for one dimension,
// honoring its gate mode. The scalar total is always checked (with
// tolerance); the per-file / per-key check on top stops a localized
// regression from hiding behind a net-zero total.
func RegressionsOf(spec GateSpec, base, cur MetricSample) []string {
	var out []string
	if Worse(spec.Direction, base.Value, cur.Value, spec.Tolerance) {
		out = append(out, fmt.Sprintf("total %s → %s", FormatNumber(base.Value), FormatNumber(cur.Value)))
	}
	switch spec.Gate {
	case GatePerFileCount:
		for _, r := range perFileRegressions(base.Breakdown, cur.Breakdown) {
			out = append(out, fmt.Sprintf("%s  %s → %s", r.File, FormatNumber(r.Base), FormatNumber(r.Current)))
		}
	case GatePerKeyValue:
		for _, k := range sortedKeys(base.Breakdown) {
			cv, ok := cur.Breakdown[k]
			if ok && Worse(spec.Direction, base.Breakdown[k], cv, spec.Tolerance) {
				out = append(out, fmt.Sprintf("%s  %s → %s", k, FormatNumber(base.Breakdown[k]), FormatNumber(cv)))
			}
		}
	}
	return out
}

// Regression is one machine-readable regression detail — the structured form
// behind a `--format json` verdict and the unit `--since` scopes to changed
// lines. For a `total` regression Key/Path/Line are nil; for per-file-count /
// per-key-value they are parsed from the breakdown key. Base/Current are the two
// compared numbers (scalar values for total, offender counts for per-file-count,
// the key's values for per-key-value). Message is the exact text-mode detail
// line. Suppressed is set only by `--since` scoping.
type Regression struct {
	Kind       string  `json:"kind"`
	Key        *string `json:"key"`
	Path       *string `json:"path"`
	Line       *int    `json:"line"`
	Base       float64 `json:"base"`
	Current    float64 `json:"current"`
	Message    string  `json:"message"`
	Suppressed bool    `json:"suppressed"`
}

// StructuredRegressions is the machine-readable twin of RegressionsOf. It emits
// one entry per new or increased offender key that contributes to a regressed
// file (per-file-count), or worsened baseline key (per-key-value) — the
// granularity `--since` needs to test each against changed lines — plus a
// `total` entry when the scalar regressed. The scalar and per-key predicates
// match RegressionsOf exactly, so the two views never disagree on whether a
// dimension regressed.
func StructuredRegressions(spec GateSpec, base, cur MetricSample) []Regression {
	var out []Regression
	if Worse(spec.Direction, base.Value, cur.Value, spec.Tolerance) {
		out = append(out, Regression{
			Kind:    "total",
			Base:    base.Value,
			Current: cur.Value,
			Message: fmt.Sprintf("total %s → %s", FormatNumber(base.Value), FormatNumber(cur.Value)),
		})
	}
	switch spec.Gate {
	case GatePerFileCount:
		for _, r := range perFileRegressions(base.Breakdown, cur.Breakdown) {
			for _, key := range r.Contributors {
				baseValue := base.Breakdown[key]
				currentValue := cur.Breakdown[key]
				out = append(out, keyRegression("per-file-count", key, baseValue, currentValue,
					fmt.Sprintf("%s  %s → %s (file total %s → %s)",
						key, FormatNumber(baseValue), FormatNumber(currentValue),
						FormatNumber(r.Base), FormatNumber(r.Current))))
			}
		}
	case GatePerKeyValue:
		for _, k := range sortedKeys(base.Breakdown) {
			if cv, ok := cur.Breakdown[k]; ok && Worse(spec.Direction, base.Breakdown[k], cv, spec.Tolerance) {
				out = append(out, keyRegression("per-key-value", k, base.Breakdown[k], cv,
					fmt.Sprintf("%s  %s → %s", k, FormatNumber(base.Breakdown[k]), FormatNumber(cv))))
			}
		}
	}
	return out
}

// keyRegression builds a per-key Regression, parsing path and 1-based line from
// the "path:line" breakdown key (Line stays nil when the key carries no numeric
// line — a location that `--since` cannot attribute to a changed line).
func keyRegression(kind, key string, base, cur float64, message string) Regression {
	file, line := splitLocationKey(key)
	r := Regression{Kind: kind, Key: strPtr(key), Path: strPtr(file), Base: base, Current: cur, Message: message}
	r.Line = line
	return r
}

// splitLocationKey parses the final :<line> suffix so paths containing colons
// (notably Windows drive paths) remain intact. A key without a numeric suffix is
// a file-only location.
func splitLocationKey(key string) (string, *int) {
	colon := strings.LastIndex(key, ":")
	if colon < 0 || colon == len(key)-1 {
		return key, nil
	}
	n, err := strconv.Atoi(key[colon+1:])
	if err != nil {
		return key, nil
	}
	return key[:colon], &n
}

// statusName is the emoji-free status word for a dimension — the `--format json`
// twin of statusOf's decorated table cell.
func statusName(direction Direction, base *float64, cur, tolerance float64) string {
	if base == nil {
		return "new"
	}
	if Worse(direction, *base, cur, tolerance) {
		return "worse"
	}
	if Worse(direction, *base, cur, 0) {
		return "within-tolerance"
	}
	if Better(direction, *base, cur) {
		return "better"
	}
	return "same"
}

func strPtr(s string) *string { return &s }

// OrphanedMetrics returns the sorted ids of baseline metrics that no
// configured dimension claims. Deleting a dimension must also drop its
// metric from the snapshot, or a regression could hide behind a vanished
// measurement.
func OrphanedMetrics(dimensionIDs []string, baseline map[string]Metric) []string {
	claimed := map[string]bool{}
	for _, id := range dimensionIDs {
		claimed[id] = true
	}
	var out []string
	for id := range baseline {
		if !claimed[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// GuardViolation is one metric that worsened between two recorded snapshots,
// as compared directly (not via a fresh measurement) — the shape
// `pawl guard` needs to decide whether a Pawl-Accept trailer covers it.
type GuardViolation struct {
	ID        string
	Direction Direction
	Base      float64
	Current   float64
	Tolerance float64
}

func (v GuardViolation) String() string {
	return fmt.Sprintf("%s: %s → %s", v.ID, FormatNumber(v.Base), FormatNumber(v.Current))
}

// GuardViolations compares two recorded snapshots' metrics directly:
// violations are metrics that worsened per their recorded direction (empty
// direction reads as lower-is-better, the conservative default for
// hand-crafted snapshots) and recorded tolerance; removed are metrics present
// in base but missing from pr. A metric only in pr has no baseline to
// violate and is ignored. Both lists are sorted by id.
func GuardViolations(base, pr map[string]Metric) (violations []GuardViolation, removed []string) {
	for _, id := range sortedMetricKeys(base) {
		b := base[id]
		p, ok := pr[id]
		if !ok {
			removed = append(removed, id)
			continue
		}
		if !definitionsCompatible(b.Definition, p.Definition) {
			continue
		}
		direction := b.Direction
		if direction == "" {
			direction = LowerIsBetter
		}
		tolerance := 0.0
		if b.Tolerance != nil {
			tolerance = *b.Tolerance
		}
		if Worse(direction, b.Value, p.Value, tolerance) {
			violations = append(violations, GuardViolation{
				ID: id, Direction: direction, Base: b.Value, Current: p.Value, Tolerance: tolerance,
			})
		}
	}
	return violations, removed
}

// RedefinedMetrics lists metric ids whose recorded measurement definition
// changed between snapshots. Their numbers are intentionally excluded from
// guard comparison because they are not values on the same scale.
func RedefinedMetrics(base, current map[string]Metric) []string {
	var out []string
	for _, id := range sortedMetricKeys(base) {
		before := base[id]
		after, ok := current[id]
		if ok && !definitionsCompatible(before.Definition, after.Definition) {
			out = append(out, id)
		}
	}
	return out
}

// WorseDimension is one configured dimension whose fresh measurement
// regressed against the committed baseline, found by the same gate-mode-aware
// predicate check's exit code uses (not the scalar-only table status) — so
// record's write gate never disagrees with what check would fail on.
type WorseDimension struct {
	ID      string
	Base    float64
	Current float64
}

// WorseDimensions scans dims for a regression between baseline and current,
// skipping any dimension with no baseline entry (nothing to regress against)
// or no fresh measurement (not part of this record). It is the single
// predicate behind record's default refusal to silently write a worse value.
func WorseDimensions(dims []Dimension, baseline, current map[string]Metric) []WorseDimension {
	var out []WorseDimension
	for _, dim := range dims {
		b, ok := baseline[dim.ID]
		if !ok {
			continue
		}
		c, ok := current[dim.ID]
		if !ok {
			continue
		}
		regressions := StructuredRegressions(dim.GateSpecOf(),
			MetricSample{Value: b.Value, Breakdown: b.Breakdown},
			MetricSample{Value: c.Value, Breakdown: c.Breakdown})
		if len(regressions) > 0 {
			out = append(out, WorseDimension{ID: dim.ID, Base: b.Value, Current: c.Value})
		}
	}
	return out
}

// SnapshotShapeErrors validates a parsed (json.Unmarshal into any) snapshot's
// shape — valid JSON alone is not a trustworthy baseline: a truncated or
// hand-corrupted snapshot must not read as "consistent" for free.
func SnapshotShapeErrors(parsed any) []string {
	obj, ok := parsed.(map[string]any)
	if !ok {
		return []string{"snapshot is not an object"}
	}
	metrics, ok := obj["metrics"].(map[string]any)
	if !ok {
		return []string{"snapshot.metrics is missing or not an object"}
	}
	if len(metrics) == 0 {
		return []string{"snapshot.metrics is empty"}
	}
	var errs []string
	for _, id := range sortedKeysAny(metrics) {
		metric, ok := metrics[id].(map[string]any)
		if !ok {
			errs = append(errs, fmt.Sprintf("metric %q is not an object", id))
			continue
		}
		if _, ok := metric["value"].(float64); !ok {
			errs = append(errs, fmt.Sprintf("metric %q has no numeric value", id))
		}
		if raw, exists := metric["definition"]; exists {
			definition, ok := raw.(string)
			if !ok || !validDefinitionFingerprint(definition) {
				errs = append(errs, fmt.Sprintf("metric %q has an invalid definition fingerprint", id))
			}
		}
	}
	return errs
}

// ImprovementNotice is the CI annotation naming every dimension that improved
// since the snapshot — a developer who only looked at check's exit code still
// finds out an improvement is sitting unrecorded. Empty off-CI or when
// nothing improved.
func ImprovementNotice(improvedIDs []string, onCI bool) string {
	if !onCI || len(improvedIDs) == 0 {
		return ""
	}
	return fmt.Sprintf("::notice::pawl improved: %s — run `%s` to lock in the gains.",
		strings.Join(improvedIDs, ", "), recordOnlyCommand(improvedIDs))
}

// recordOnlyCommand is the mechanical next step after a scalar improvement:
// re-record just those dimensions so a full record cannot silently bless
// regressions elsewhere.
func recordOnlyCommand(ids []string) string {
	return "pawl record --only " + strings.Join(ids, ",")
}

// GitHubAnnotations renders GitHub Actions `::error::` workflow commands for one
// regressed dimension so violations surface inline on the PR diff, reusing the
// `path:line` breakdown key shape. Per-file-count emits one line-anchored
// annotation per new or increased offender key in a file whose count rose;
// per-key-value emits one per key that worsened; a total-gate (or detail-less)
// regression emits a single file-less annotation. Empty when nothing regressed.
func GitHubAnnotations(id, title string, spec GateSpec, base, cur MetricSample) []string {
	var out []string
	switch spec.Gate {
	case GatePerFileCount:
		for _, r := range perFileRegressions(base.Breakdown, cur.Breakdown) {
			for _, key := range r.Contributors {
				detail := "new " + id + " offender"
				if baseValue, existed := base.Breakdown[key]; existed {
					detail = FormatNumber(baseValue) + " → " + FormatNumber(cur.Breakdown[key])
				}
				out = append(out, annotationLine(id, title, key, detail))
			}
		}
	case GatePerKeyValue:
		for _, k := range sortedKeys(base.Breakdown) {
			if cv, ok := cur.Breakdown[k]; ok && Worse(spec.Direction, base.Breakdown[k], cv, spec.Tolerance) {
				out = append(out, annotationLine(id, title, k, FormatNumber(base.Breakdown[k])+" → "+FormatNumber(cv)))
			}
		}
	}
	if len(out) == 0 && Worse(spec.Direction, base.Value, cur.Value, spec.Tolerance) {
		out = append(out, fmt.Sprintf("::error title=pawl: %s::%s regressed: %s → %s",
			id, title, FormatNumber(base.Value), FormatNumber(cur.Value)))
	}
	return out
}

// annotationLine builds one `::error file=…,line=…::` command from a
// "path:line" breakdown key (the line clause is dropped when the key carries
// no numeric line suffix).
func annotationLine(id, title, key, detail string) string {
	file, line := splitLocationKey(key)
	loc := "file=" + file
	if line != nil {
		loc += ",line=" + strconv.Itoa(*line)
	}
	return fmt.Sprintf("::error %s,title=pawl: %s::%s: %s", loc, id, title, detail)
}

func sortedKeys(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysAny(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedMetricKeys(m map[string]Metric) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeyUnion(a, b map[string]float64) []string {
	seen := map[string]bool{}
	var out []string
	for k := range a {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for k := range b {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
