#!/usr/bin/env python3
"""Tests for the two scripts that score an eval run.

These exist because both scripts silently mis-scored a real run. The
invocation counter classified `which pawl` as a gate check and, once `pawl
measure` was added to the CLI, classified that as one too — together turning
"measured twice, never checked" into a verifies-against-the-gate PASS. The
contamination auditor flagged a command scoped *into* the eval directory as
one that had escaped it, because /tmp and /private/tmp are the same directory
spelled two ways.

Both are judgement scripts: when they are wrong, the number they print looks
exactly as authoritative as when they are right, and the run it describes is
usually gone by the time anyone doubts it. So they get tests.

    python3 demo/scripts/test_eval_scripts.py
"""
import importlib.util
import json
import os
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))


def load(filename, name):
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


invocations = load("pawl-invocations.py", "invocations")
audit = load("audit-eval-transcript.py", "audit")
ab_runner = load("run-agent-ab.py", "ab_runner")


def transcript(*tool_uses):
    """Write a session JSONL holding the given tool calls, in order."""
    content = [{"type": "tool_use", "name": name, "input": args} for name, args in tool_uses]
    line = json.dumps({"message": {"role": "assistant", "content": content}})
    fd, path = tempfile.mkstemp(suffix=".jsonl")
    with os.fdopen(fd, "w") as f:
        f.write(line + "\n")
    return path


def codex_transcript(*items):
    fd, path = tempfile.mkstemp(suffix=".jsonl")
    with os.fdopen(fd, "w") as f:
        for item in items:
            f.write(json.dumps({"type": "item.completed", "item": item}) + "\n")
    return path


def bash(cmd):
    return ("Bash", {"command": cmd})


def edit(path):
    return ("Edit", {"file_path": path})


def timeline(*tool_uses):
    path = transcript(*tool_uses)
    try:
        return list(invocations.events(path))
    finally:
        os.unlink(path)


def calls(*tool_uses):
    return [d for kind, d in timeline(*tool_uses) if kind == "pawl"]


def subcommands(*tool_uses):
    return [invocations.describe(c)[0] for c in calls(*tool_uses)]


class TestWhatCountsAsAnInvocation(unittest.TestCase):
    def test_looking_pawl_up_is_not_running_it(self):
        self.assertEqual(calls(bash("which pawl")), [])
        self.assertEqual(calls(bash("command -v pawl")), [])
        self.assertEqual(calls(bash("ls -l ~/.local/bin/pawl")), [])

    def test_a_repo_path_containing_pawl_is_not_an_invocation(self):
        self.assertEqual(calls(bash("cd /Volumes/jdisk/code/pawl && ls")), [])
        self.assertEqual(calls(bash("cat /tmp/-Volumes-jdisk-code-pawl/notes.txt")), [])

    def test_an_invocation_after_a_shell_separator_counts(self):
        self.assertEqual(subcommands(bash("cd /tmp/eval && pawl check")), ["check"])
        self.assertEqual(subcommands(bash("pawl check; pawl record")), ["check", "record"])
        self.assertEqual(subcommands(bash("pawl check --format json | jq .")), ["check"])

    def test_env_and_wrapper_prefixes_still_count(self):
        self.assertEqual(subcommands(bash("PAWL_ROOT=/x pawl check")), ["check"])
        self.assertEqual(subcommands(bash("cd /tmp/eval && time pawl check")), ["check"])


class TestWhatCountsAsVerification(unittest.TestCase):
    def test_measure_is_not_a_check(self):
        # `measure` prints numbers and no verdict, so an agent that runs it and
        # then calls a dimension "improved" compared against something other
        # than the baseline. Crediting it here is what hid exactly that run.
        self.assertEqual(subcommands(bash("cd /tmp/eval && pawl measure")), ["measure"])

    def test_global_flags_before_command_do_not_hide_measure(self):
        self.assertEqual(invocations.describe("pawl --format json measure --only m")[0], "measure")
        self.assertEqual(invocations.describe("pawl -c custom.yaml check --format json")[0], "check")

    def test_version_and_help_are_not_a_check(self):
        self.assertEqual(subcommands(bash("pawl --version")), ["version"])
        self.assertEqual(subcommands(bash("pawl --help")), ["help"])

    def test_a_bare_pawl_invocation_is_the_default_check(self):
        self.assertEqual(subcommands(bash("cd /tmp/eval && pawl")), ["check (default)"])

    def test_measuring_after_an_edit_does_not_verify(self):
        events = timeline(edit("/tmp/eval/src/a.js"), bash("cd /tmp/eval && pawl measure"))
        self.assertEqual([k for k, _ in events], ["edit", "pawl"])
        last_check = [i for i, (k, d) in enumerate(events)
                      if k == "pawl" and invocations.describe(d)[0] in ("check", "check (default)")]
        self.assertEqual(last_check, [], "measure must not satisfy the verification item")

    def test_checking_after_an_edit_verifies(self):
        events = timeline(edit("/tmp/eval/src/a.js"), bash("cd /tmp/eval && pawl check"))
        last_check = [i for i, (k, d) in enumerate(events)
                      if k == "pawl" and invocations.describe(d)[0] in ("check", "check (default)")]
        self.assertEqual(last_check, [1])


class TestEditDetection(unittest.TestCase):
    def test_a_shell_redirect_is_an_edit(self):
        kinds = [k for k, _ in timeline(bash("cat > src/a.js <<'EOF'\nx\nEOF"))]
        self.assertIn("edit", kinds)

    def test_writing_the_measurement_document_is_not_a_source_edit(self):
        # `pawl measure > current.json` redirects, but the pawl call on the same
        # line means the agent is measuring, not editing source.
        kinds = [k for k, _ in timeline(bash("cd /tmp/eval && pawl measure > current.json"))]
        self.assertEqual(kinds, ["pawl"])


class TestAgentABHarness(unittest.TestCase):
    def test_task_prompt_is_read_from_authoritative_task_file(self):
        self.assertIn("Add a `median` helper", ab_runner.task_prompt(5))
        self.assertIn("passing-tests", ab_runner.task_prompt(6))
        self.assertNotIn("##", ab_runner.task_prompt(6))

    def test_treatment_setup_preflights_pawl_and_strips_spoilers(self):
        with tempfile.TemporaryDirectory() as tmp:
            fake = os.path.join(tmp, "fake-pawl")
            with open(fake, "w") as f:
                f.write("#!/bin/sh\n"
                        "case \"$1\" in\n"
                        "  version) echo 'pawl eval';;\n"
                        "  agent) printf '<!-- pawl:begin -->\\nblock\\n<!-- pawl:end -->\\n' > AGENTS.md;;\n"
                        "  *) exit 2;;\n"
                        "esac\n")
            os.chmod(fake, 0o755)
            root = os.path.join(tmp, "runs")
            os.mkdir(root)
            work, _ = ab_runner.prepare_arm(ab_runner.Path(root), "treatment", ab_runner.Path(fake))
            self.assertFalse((work / "TASKS.md").exists())
            self.assertTrue((work / "AGENTS.md").exists())
            self.assertTrue((work / ".eval-bin" / "pawl").exists())


class TestCodexTranscriptSupport(unittest.TestCase):
    def test_command_and_file_change_feed_the_same_timeline(self):
        path = codex_transcript(
            {"type": "file_change", "changes": [{"path": "/tmp/eval/src/a.js"}]},
            {"type": "command_execution", "command": "/bin/zsh -lc 'pawl check --format json'"},
        )
        try:
            self.assertEqual(list(invocations.events(path)), [
                ("edit", "Edit /tmp/eval/src/a.js"),
                ("pawl", "pawl check --format json"),
            ])
        finally:
            os.unlink(path)

    def test_multiline_shell_payload_counts_later_invocations(self):
        path = codex_transcript({
            "type": "command_execution",
            "command": "/bin/zsh -lc 'status=0\npawl check --only m || status=$?\npawl record --only m\nexit $status'",
        })
        try:
            self.assertEqual([invocations.describe(d)[0] for k, d in invocations.events(path) if k == "pawl"],
                             ["check", "record"])
        finally:
            os.unlink(path)

    def test_auditor_reads_codex_command_items(self):
        path = codex_transcript({
            "type": "command_execution",
            "command": "cat /Volumes/jdisk/code/pawl/demo/capabilities.yaml",
        })
        try:
            self.assertEqual(list(audit.bash_commands(path)), [
                "cat /Volumes/jdisk/code/pawl/demo/capabilities.yaml",
            ])
        finally:
            os.unlink(path)


class TestContaminationAudit(unittest.TestCase):
    EVAL = "/private/tmp/scratch/eval/t5"
    REPO = "/Volumes/jdisk/code/pawl"

    def test_the_same_directory_spelled_two_ways_is_not_an_escape(self):
        # /tmp is a symlink to /private/tmp on macOS: the scratchpad path a
        # harness is handed is the /private form, while a task prompt usually
        # says /tmp. Comparing the spellings instead of the directories
        # discarded clean runs as contaminated.
        self.assertFalse(audit.is_violation(
            "cd /tmp/scratch/eval/t5 && cat capabilities.yaml", self.EVAL, self.REPO))
        self.assertFalse(audit.is_violation(
            "cd /private/tmp/scratch/eval/t5 && cat capabilities.yaml", self.EVAL, self.REPO))

    def test_sibling_directory_with_repo_name_prefix_is_not_an_escape(self):
        eval_dir = "/Volumes/jdisk/code/pawl-ab/control"
        self.assertFalse(audit.is_violation(
            f"cd {eval_dir} && pawl check --format json", eval_dir, self.REPO))

    def test_touching_the_real_repo_is_an_escape(self):
        self.assertTrue(audit.is_violation(
            "cat /Volumes/jdisk/code/pawl/demo/capabilities.yaml", self.EVAL, self.REPO))

    def test_reading_a_spoiler_outside_the_eval_dir_is_an_escape(self):
        self.assertTrue(audit.is_violation("cat demo/README.md", self.EVAL, self.REPO))
        self.assertTrue(audit.is_violation("cat fixture/TASKS.md", self.EVAL, self.REPO))

    def test_ordinary_work_inside_the_eval_dir_is_clean(self):
        self.assertFalse(audit.is_violation(
            "cd /tmp/scratch/eval/t5 && pawl check --format json", self.EVAL, self.REPO))


if __name__ == "__main__":
    unittest.main(verbosity=2)
