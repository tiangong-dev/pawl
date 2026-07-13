package pawl_test

import (
	"testing"

	pawl "github.com/tiangong-dev/pawl/internal/pawl"
)

// ImprovementNotice is the CI annotation surfacing an unrecorded win on the
// PR itself. It is empty off-CI or when nothing improved, and on CI prints
// the exact pinned annotation line naming every improved dimension.
func TestImprovementNotice(t *testing.T) {
	cases := []struct {
		name string
		ids  []string
		onCI bool
		want string
	}{
		{"no improvements, off CI", nil, false, ""},
		{"no improvements, on CI", nil, true, ""},
		{"empty slice (not nil), on CI", []string{}, true, ""},
		{"improvements but off CI", []string{"a", "b"}, false, ""},
		{
			"single improvement on CI",
			[]string{"a"},
			true,
			"::notice::pawl improved: a — run `pawl record` to lock in the gains.",
		},
		{
			"multiple improvements on CI",
			[]string{"a", "b"},
			true,
			"::notice::pawl improved: a, b — run `pawl record` to lock in the gains.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pawl.ImprovementNotice(tc.ids, tc.onCI)
			if got != tc.want {
				t.Errorf("ImprovementNotice(%v, %v) = %q, want %q", tc.ids, tc.onCI, got, tc.want)
			}
		})
	}
}
