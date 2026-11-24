package pawl

import "testing"

// The rename records come from `git diff --name-status -z`, where each rename is
// three NUL-separated fields: the "R<score>" status, the source path, the
// destination path. Reading them as anything but whole triples would hand the
// paths to the wrong roles.
func TestParseRenameRecords(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want [][2]string
	}{
		{name: "no output", in: "", want: nil},
		{name: "one rename", in: "R100\x00a.go\x00b.go\x00", want: [][2]string{{"a.go", "b.go"}}},
		{
			name: "several renames in one diff",
			in:   "R100\x00a.go\x00b.go\x00R090\x00c.go\x00d.go\x00",
			want: [][2]string{{"a.go", "b.go"}, {"c.go", "d.go"}},
		},
		{
			name: "paths keep spaces and non-ASCII bytes",
			in:   "R100\x00svc 服务/a b.go\x00svc 服务/文件 c.go\x00",
			want: [][2]string{{"svc 服务/a b.go", "svc 服务/文件 c.go"}},
		},
		{
			name: "a truncated record yields nothing rather than misaligned paths",
			in:   "R100\x00a.go\x00",
			want: nil,
		},
		{
			name: "a status that is not a rename stops the parse",
			in:   "A\x00new.go\x00R100\x00a.go\x00b.go\x00",
			want: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRenameRecords(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("parseRenameRecords(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("parseRenameRecords(%q)[%d] = %v, want %v", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}
