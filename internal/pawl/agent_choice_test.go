package pawl

import (
	"os"
	"strings"
	"testing"
)

// The prompt only runs when a human is on both ends of the pipe, so the terminal check itself is not exercised here; what is exercised is the answer handling, where a wrong reading writes a file the user did not choose.
func TestChooseAgentTargetReadsAnAnswer(t *testing.T) {
	for _, tc := range []struct {
		answer   string
		want     string
		wantOK   bool
		wantFile string
	}{
		{answer: "1\n", want: "agent", wantOK: true, wantFile: "AGENTS.md"},
		{answer: "2\n", want: "claude", wantOK: true, wantFile: "CLAUDE.md"},
		{answer: "3\n", want: "", wantOK: true},
		{answer: " 2 \n", want: "claude", wantOK: true, wantFile: "CLAUDE.md"},
		{answer: "claude\n", want: "claude", wantOK: true, wantFile: "CLAUDE.md"},
		{answer: "AGENTS.md\n", want: "agent", wantOK: true, wantFile: "AGENTS.md"},
	} {
		t.Run(strings.TrimSpace(tc.answer), func(t *testing.T) {
			var prompt strings.Builder
			got, ok := chooseAgentTarget(strings.NewReader(tc.answer), &prompt)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("chooseAgentTarget(%q) = (%q, %v), want (%q, %v)", tc.answer, got, ok, tc.want, tc.wantOK)
			}
			if tc.wantFile != "" && !strings.Contains(prompt.String(), tc.wantFile) {
				t.Errorf("the prompt never offers %s:\n%s", tc.wantFile, prompt.String())
			}
		})
	}
}

// Both instruction files have to appear in the prompt with the tools that read them: the whole reason for the choice is that Claude Code does not read AGENTS.md, and a prompt that hides that makes the wrong pick look right.
func TestChooseAgentTargetPromptNamesTheTools(t *testing.T) {
	var prompt strings.Builder
	chooseAgentTarget(strings.NewReader("3\n"), &prompt)

	for _, want := range []string{"AGENTS.md", "Codex", "Antigravity", "Cursor", "CLAUDE.md", "Claude Code"} {
		if !strings.Contains(prompt.String(), want) {
			t.Errorf("the prompt never mentions %q:\n%s", want, prompt.String())
		}
	}
}

// An unusable answer is re-asked rather than resolved to a default, since the default would be a file write nobody chose.
func TestChooseAgentTargetReAsksOnAnUnusableAnswer(t *testing.T) {
	var prompt strings.Builder
	got, ok := chooseAgentTarget(strings.NewReader("\nnope\n2\n"), &prompt)
	if !ok || got != "claude" {
		t.Fatalf("chooseAgentTarget = (%q, %v), want (\"claude\", true)", got, ok)
	}
	if n := strings.Count(prompt.String(), "Where should the pawl block go?"); n != 3 {
		t.Errorf("prompt shown %d times, want 3 (two rejected answers, then the accepted one):\n%s", n, prompt.String())
	}
}

// EOF is not an answer. Picking a target here would write a file the user never chose, on a stream that stopped talking.
func TestChooseAgentTargetFailsOnEOF(t *testing.T) {
	var prompt strings.Builder
	got, ok := chooseAgentTarget(strings.NewReader(""), &prompt)
	if ok {
		t.Fatalf("chooseAgentTarget on EOF = (%q, true), want ok=false", got)
	}
	if !strings.Contains(prompt.String(), "--write") {
		t.Errorf("the failure does not point at the non-interactive way in:\n%s", prompt.String())
	}
}

// endlessAnswers is a stream that looks like a terminal to a reader and never stops talking, which is what a character device that is not a tty behaves like.
type endlessAnswers struct{ line string }

func (e endlessAnswers) Read(p []byte) (int, error) { return copy(p, e.line), nil }

// A stream that answers forever must not be prompted forever.
func TestChooseAgentTargetGivesUpOnEndlessUnusableAnswers(t *testing.T) {
	var prompt strings.Builder
	got, ok := chooseAgentTarget(endlessAnswers{line: "nope\n"}, &prompt)
	if ok {
		t.Fatalf("chooseAgentTarget = (%q, true), want ok=false", got)
	}
	if !strings.Contains(prompt.String(), "--write") {
		t.Errorf("giving up does not point at the non-interactive way in:\n%s", prompt.String())
	}
}

// /dev/null is a character device, so the mode bit alone calls it a terminal. Redirecting to it is how a CI step detaches a command, and treating that as interactive would stop the run to ask a question nobody can answer.
func TestDevNullIsNotATerminal(t *testing.T) {
	for _, flag := range []int{os.O_RDONLY, os.O_WRONLY} {
		f, err := os.OpenFile(os.DevNull, flag, 0)
		if err != nil {
			t.Fatalf("opening %s: %v", os.DevNull, err)
		}
		if isTerminalFile(f) {
			t.Errorf("%s (flag %d) reads as a terminal", os.DevNull, flag)
		}
		f.Close()
	}
}

func TestMergeAgentBlock(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing string
		wantVerb string
		wantKeep []string
	}{
		{name: "empty file", existing: "", wantVerb: "wrote"},
		{name: "whitespace only", existing: "\n\n", wantVerb: "wrote"},
		{name: "prose", existing: "# Rules\n\nRun the linter.\n", wantVerb: "appended to", wantKeep: []string{"# Rules", "Run the linter."}},
		{
			name:     "installed block",
			existing: "# Rules\n\n" + agentBlockBeginMarker + "\nold\n" + agentBlockEndMarker + "\n\n## After\n",
			wantVerb: "updated",
			wantKeep: []string{"# Rules", "## After"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, verb, err := mergeAgentBlock(tc.existing)
			if err != nil {
				t.Fatalf("mergeAgentBlock: %v", err)
			}
			if verb != tc.wantVerb {
				t.Errorf("verb = %q, want %q", verb, tc.wantVerb)
			}
			if n := strings.Count(got, agentBlockBeginMarker); n != 1 {
				t.Errorf("result carries %d blocks, want 1:\n%s", n, got)
			}
			if !strings.Contains(got, "pawl check --format json") {
				t.Errorf("result does not carry the current block:\n%s", got)
			}
			if strings.Contains(got, "\nold\n") {
				t.Errorf("a stale block survived:\n%s", got)
			}
			for _, keep := range tc.wantKeep {
				if !strings.Contains(got, keep) {
					t.Errorf("prose %q was lost:\n%s", keep, got)
				}
			}
		})
	}
}

// A file whose markers pawl cannot read unambiguously is left alone: guessing here destroys prose pawl did not write.
func TestMergeAgentBlockRefusesDamagedMarkers(t *testing.T) {
	for name, existing := range map[string]string{
		"begin without end": agentBlockBeginMarker + "\nhalf\n",
		"end without begin": "half\n" + agentBlockEndMarker + "\n",
		"end before begin":  agentBlockEndMarker + "\nbackwards\n" + agentBlockBeginMarker + "\n",
		"two blocks": agentBlockBeginMarker + "\none\n" + agentBlockEndMarker + "\n" +
			agentBlockBeginMarker + "\ntwo\n" + agentBlockEndMarker + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := mergeAgentBlock(existing); err == nil {
				t.Fatalf("mergeAgentBlock(%q) returned no error", existing)
			}
		})
	}
}

// The block ends up separated from whatever preceded it; an append that ran the loop onto the end of a sentence would change what the previous line says.
func TestMergeAgentBlockSeparatesAnAppendFromExistingProse(t *testing.T) {
	got, _, err := mergeAgentBlock("Run the linter.")
	if err != nil {
		t.Fatalf("mergeAgentBlock: %v", err)
	}
	if !strings.Contains(got, "Run the linter.\n\n"+agentBlockBeginMarker) {
		t.Errorf("the appended block is not separated from the prose:\n%q", got)
	}
}
