package pawl

import (
	"strings"
	"testing"
)

func TestGitOutputReportsStartFailure(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, code, stderr := gitOutputRaw(t.TempDir(), "version")
	if code == 0 || !strings.Contains(stderr, "git") {
		t.Fatalf("gitOutputRaw = code %d, stderr %q; want a diagnostic when git cannot start", code, stderr)
	}
}
