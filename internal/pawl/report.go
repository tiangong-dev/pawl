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
// See SPEC.md § Machine-readable output.
type Report struct {
	SchemaVersion int            `json:"schema_version"`
	Command       string         `json:"command"`
	Mode          string         `json:"mode"`
	Since         *string        `json:"since"`
	ExitCode      int            `json:"exit_code"`
	Metrics       []MetricReport `json:"metrics"`
}

// MetricReport is one dimension's verdict. Base is nil for a new dimension
// (no baseline). EnforcedInFull is set by `--since` for a total-gate dimension
// that cannot be scoped to changed lines and is therefore kept at full strength;
// it is not serialized (it drives only the text banner).
type MetricReport struct {
	ID             string       `json:"id"`
	Title          string       `json:"title"`
	Direction      Direction    `json:"direction"`
	Gate           GateMode     `json:"gate"`
	Unit           string       `json:"unit"`
	Base           *float64     `json:"base"`
	Current        float64      `json:"current"`
	Status         string       `json:"status"`
	Improved       bool         `json:"improved"`
	Regressions    []Regression `json:"regressions"`
	EnforcedInFull bool         `json:"-"`
}

// buildReport assembles the full-mode verdict for a command from the baseline
// and a fresh measurement. Metrics are sorted by id (a stable machine contract).
// The gate defaults to "total" in the output when a dimension left it unset.
func buildReport(command string, cfg *Config, baseline *Snapshot, current map[string]Metric) *Report {
	rep := &Report{SchemaVersion: 1, Command: command, Mode: "full"}
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
			ID:          dim.ID,
			Title:       dim.Title,
			Direction:   dim.Direction,
			Gate:        gate,
			Unit:        cur.Unit,
			Current:     cur.Value,
			Regressions: []Regression{},
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

// renderReportJSON writes the verdict as one indented JSON object plus a
// trailing newline — stdout stays pure machine output (no table, no emoji, no
// GitHub annotations).
func renderReportJSON(w io.Writer, rep *Report) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}
