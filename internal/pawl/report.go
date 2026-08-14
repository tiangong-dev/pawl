package pawl

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// Report is pawl's stable machine-readable verdict — the `--format json` shape.
// It is deliberately not rdjson: rdjson cannot express a scalar total, an
// improvement, or a `--since` suppression, which are pawl's own semantics.
// See spec/engine/verdict.md § Machine-readable output.
type Report struct {
	SchemaVersion int                  `json:"schema_version"`
	Command       string               `json:"command"`
	Mode          string               `json:"mode"`
	Since         *string              `json:"since"`
	DryRun        bool                 `json:"dry_run,omitempty"`
	AcceptedWorse []AcceptedWorseEntry `json:"accepted_worse,omitempty"`
	ExitCode      int                  `json:"exit_code"`
	FailureClass  string               `json:"failure_class,omitempty"`
	Error         string               `json:"error,omitempty"`
	FailedMetrics []string             `json:"failed_metrics,omitempty"`
	Watch         []WatchEntry         `json:"watch,omitempty"`
	Metrics       []MetricReport       `json:"metrics"`
}

// WatchEntry is one near- or over-threshold file the current invocation
// touched — advisory headroom, never part of the exit-code verdict.
type WatchEntry struct {
	ID        string  `json:"id"`
	Path      string  `json:"path"`
	Kind      string  `json:"kind"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	Headroom  float64 `json:"headroom"`
	Status    string  `json:"status"`
}

// AcceptedWorseEntry is one dimension record wrote (or, under --dry-run,
// would write) worse than the committed baseline — the machine-readable twin
// of the `Pawl-Accept: <id> <value>` trailer hint text mode prints, so a
// `--format json`/`--format codeclimate` caller can build the same commit
// trailer without re-deriving it from Metrics/status.
type AcceptedWorseEntry struct {
	ID    string  `json:"id"`
	Value float64 `json:"value"`
}

// acceptedWorseEntries converts the write gate's WorseDimension list (in the
// order accepted, i.e. only ever populated when --accept-worse or --dry-run
// let the write proceed) into the Report-facing shape.
func acceptedWorseEntries(worse []WorseDimension) []AcceptedWorseEntry {
	if len(worse) == 0 {
		return nil
	}
	out := make([]AcceptedWorseEntry, 0, len(worse))
	for _, w := range worse {
		out = append(out, AcceptedWorseEntry{ID: w.ID, Value: w.Current})
	}
	return out
}

// MetricReport is one dimension's verdict. Base is nil for a new dimension
// (no baseline). EnforcedInFull is set by `--since` for a total-gate dimension
// that cannot be scoped to changed lines and is therefore kept at full strength;
// it is not serialized (it drives only the text banner).
type MetricReport struct {
	ID               string       `json:"id"`
	Title            string       `json:"title"`
	Direction        Direction    `json:"direction"`
	Gate             GateMode     `json:"gate"`
	Unit             string       `json:"unit"`
	Base             *float64     `json:"base"`
	Current          *float64     `json:"current"`
	SnapshotValue    *float64     `json:"snapshot_value,omitempty"`
	MeasurementState string       `json:"measurement_state"`
	Status           string       `json:"status"`
	Improved         bool         `json:"improved"`
	NextAction       string       `json:"next_action,omitempty"`
	Regressions      []Regression `json:"regressions"`
	EnforcedInFull   bool         `json:"-"`
}

// buildReport assembles the full-mode verdict for a command from the baseline
// and a fresh measurement. Metrics are sorted by id (a stable machine contract).
// The gate defaults to "total" in the output when a dimension left it unset.
func buildReport(command string, cfg *Config, baseline *Snapshot, current map[string]Metric) *Report {
	rep := &Report{SchemaVersion: 2, Command: command, Mode: "full"}
	if baseline == nil {
		baseline = &Snapshot{} // first record: no baseline to compare against.
	}
	dims := append([]Dimension(nil), cfg.Dimensions...)
	sort.Slice(dims, func(i, j int) bool { return dims[i].ID < dims[j].ID })
	for _, dim := range dims {
		cur := current[dim.ID]
		gate := dim.Gate
		if gate == "" {
			gate = GateTotal
		}
		tolerance := 0.0
		if dim.Tolerance != nil {
			tolerance = *dim.Tolerance
		}
		m := MetricReport{
			ID:               dim.ID,
			Title:            dim.Title,
			Direction:        dim.Direction,
			Gate:             gate,
			Unit:             cur.Unit,
			Current:          floatPtr(cur.Value),
			MeasurementState: "measured",
			Regressions:      []Regression{},
		}
		if b, ok := baseline.Metrics[dim.ID]; ok {
			base := b.Value
			m.Base = &base
			m.Improved = Better(dim.Direction, base, cur.Value)
			m.Regressions = StructuredRegressions(dim.GateSpecOf(),
				MetricSample{Value: base, Breakdown: b.Breakdown},
				MetricSample{Value: cur.Value, Breakdown: cur.Breakdown})
			if m.Regressions == nil {
				m.Regressions = []Regression{}
			}
		}
		m.Status = statusName(dim.Direction, m.Base, cur.Value, tolerance)
		if len(m.Regressions) > 0 {
			// A per-file/per-key regression can leave the scalar unchanged; the
			// gate's verdict overrides the scalar-only status (as the text table does).
			m.Status = "worse"
		}
		rep.Metrics = append(rep.Metrics, m)
	}
	return rep
}

// buildPartialRecordReport distinguishes observations made by this invocation
// from values copied out of the old snapshot. A preserved value is written to
// SnapshotValue while Current is null: it was recorded, but it was not current
// evidence. wroteToDisk must be false for a refused or --dry-run invocation —
// a measured dimension's SnapshotValue means "what is now on disk", which is
// only true once the write actually happened; a preserved dimension's
// SnapshotValue is the committed value either way, since a preserved metric
// is by definition identical to what's already on disk.
func buildPartialRecordReport(cfg *Config, baseline *Snapshot, measured, merged map[string]Metric, wroteToDisk bool) *Report {
	rep := &Report{SchemaVersion: 2, Command: "record", Mode: "full"}
	dims := append([]Dimension(nil), cfg.Dimensions...)
	sort.Slice(dims, func(i, j int) bool { return dims[i].ID < dims[j].ID })
	for _, dim := range dims {
		mergedValue, ok := merged[dim.ID]
		if !ok {
			continue
		}
		gate := dim.Gate
		if gate == "" {
			gate = GateTotal
		}
		m := MetricReport{
			ID:          dim.ID,
			Title:       dim.Title,
			Direction:   dim.Direction,
			Gate:        gate,
			Unit:        mergedValue.Unit,
			Regressions: []Regression{},
		}
		baseMetric, hadBase := baseline.Metrics[dim.ID]
		if hadBase {
			m.Base = floatPtr(baseMetric.Value)
		}
		if cur, wasMeasured := measured[dim.ID]; wasMeasured {
			m.Current = floatPtr(cur.Value)
			if wroteToDisk {
				m.SnapshotValue = floatPtr(cur.Value)
			}
			m.MeasurementState = "measured"
			m.Status = statusName(dim.Direction, m.Base, cur.Value, dim.GateSpecOf().Tolerance)
			m.Improved = hadBase && Better(dim.Direction, baseMetric.Value, cur.Value)
		} else {
			m.SnapshotValue = floatPtr(mergedValue.Value)
			m.MeasurementState = "preserved"
			m.Status = "preserved"
		}
		rep.Metrics = append(rep.Metrics, m)
	}
	return rep
}

func floatPtr(v float64) *float64 { return &v }

// hasLiveRegression reports whether any metric carries a regression that is not
// suppressed by `--since` — the check-command exit-1 predicate for the report.
func hasLiveRegression(rep *Report) bool {
	for _, m := range rep.Metrics {
		for _, r := range m.Regressions {
			if !r.Suppressed {
				return true
			}
		}
	}
	return false
}

// annotateReport fills mechanical agent-facing fields from the already-computed
// verdict: failure_class mirrors the 1-vs-2 exit split, and next_action on an
// improved check/diff metric is the surgical record command that locks it in.
func annotateReport(rep *Report) {
	switch rep.ExitCode {
	case 1:
		rep.FailureClass = "regression"
	case 2:
		rep.FailureClass = "could-not-measure"
	}
	if rep.Command != "check" && rep.Command != "diff" {
		return
	}
	for i := range rep.Metrics {
		if rep.Metrics[i].Improved {
			rep.Metrics[i].NextAction = "pawl record --only " + rep.Metrics[i].ID
		}
	}
}

// renderReportJSON writes the verdict as one indented JSON object plus a
// trailing newline — stdout stays pure machine output (no table, no emoji, no
// GitHub annotations).
func renderReportJSON(w io.Writer, rep *Report) error {
	annotateReport(rep)
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

// abortCouldNotMeasure is the --format json path for an exit-2 gate: the
// human diagnostic stays on stderr, and stdout gets the same verdict object
// (failure_class: could-not-measure) so an agent does not have to parse
// unstructured stderr. Usage errors never reach here.
func abortCouldNotMeasure(command, format, msg string, failedIDs []string, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, msg)
	if format != "json" {
		return 2
	}
	rep := &Report{
		SchemaVersion: 2,
		Command:       command,
		Mode:          "full",
		ExitCode:      2,
		Error:         msg,
		FailedMetrics: failedIDs,
		Metrics:       []MetricReport{},
	}
	if err := renderReportJSON(stdout, rep); err != nil {
		fmt.Fprintln(stderr, err)
	}
	return 2
}
