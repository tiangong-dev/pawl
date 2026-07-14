// Package pawl is a language-agnostic anti-regression quality gate: each
// dimension measures one number, record snapshots the numbers, check fails when
// any dimension regresses against the snapshot. See SPEC.md for the frozen
// behavioral contract this package implements.
package pawl

type Direction string

const (
	LowerIsBetter  Direction = "lower-is-better"
	HigherIsBetter Direction = "higher-is-better"
)

// GateMode is how check compares a re-measured value against the snapshot.
// The zero value means "total".
type GateMode string

const (
	GateTotal        GateMode = "total"
	GatePerFileCount GateMode = "per-file-count"
	GatePerKeyValue  GateMode = "per-key-value"
)

// Metric is one dimension's measurement as stored in the snapshot. Breakdown
// nil maps to JSON null; Tolerance nil means the dimension declared none.
type Metric struct {
	Direction Direction          `json:"direction"`
	Value     float64            `json:"value"`
	Unit      string             `json:"unit"`
	Breakdown map[string]float64 `json:"breakdown"`
	Tolerance *float64           `json:"tolerance,omitempty"`
}

type Snapshot struct {
	Metrics map[string]Metric `json:"metrics"`
}

// MetricSample is the narrow input a regression check reads — direction and
// gate mode come from the dimension (GateSpec), never from the sample.
type MetricSample struct {
	Value     float64
	Breakdown map[string]float64
}

// GateSpec is how one dimension gates: its direction, gate mode (empty means
// total), and absolute tolerance in the worse direction.
type GateSpec struct {
	Direction Direction
	Gate      GateMode
	Tolerance float64
}
