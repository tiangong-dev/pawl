#!/usr/bin/env python3
"""Run a clean Codex control/treatment pair against demo/fixture.

The harness makes executable availability and transcript hygiene preconditions,
not things inferred after an expensive run. Results belong outside the repo:
there is deliberately no append-only score log under demo/.
"""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
FIXTURE = HERE.parent / "fixture"
TASKS = FIXTURE / "TASKS.md"


def task_prompt(number):
    text = TASKS.read_text()
    match = re.search(rf'^## Task {number} — "(.*?)"\n\n', text, re.M | re.S)
    if not match:
        raise ValueError(f"task {number} prompt not found")
    return " ".join(line.removeprefix("## ").strip() for line in match.group(1).splitlines())


def run(args, **kwargs):
    return subprocess.run(args, text=True, **kwargs)


def prepare_arm(root, arm, pawl):
    work = root / arm
    shutil.copytree(FIXTURE, work)
    (work / "TASKS.md").unlink(missing_ok=True)
    (work / "demo-junit.xml").unlink(missing_ok=True)
    bindir = work / ".eval-bin"
    bindir.mkdir()
    shutil.copy2(pawl, bindir / "pawl")
    (bindir / "pawl").chmod(0o755)
    env = os.environ.copy()
    env["PATH"] = str(bindir) + os.pathsep + env.get("PATH", "")
    probe = run(["pawl", "version"], cwd=work, env=env, capture_output=True)
    if probe.returncode != 0:
        raise RuntimeError(f"{arm}: pawl preflight failed: {probe.stderr}")

    run(["git", "init", "-q"], cwd=work, check=True)
    run(["git", "config", "user.email", "eval@example.invalid"], cwd=work, check=True)
    run(["git", "config", "user.name", "pawl eval"], cwd=work, check=True)
    if arm == "treatment":
        install = run(["pawl", "agent", "--write", "agent"], cwd=work, env=env, capture_output=True)
        if install.returncode != 0:
            raise RuntimeError(f"treatment install failed: {install.stderr}")
    run(["git", "add", "-A"], cwd=work, check=True)
    run(["git", "commit", "-qm", f"{arm} fixture"], cwd=work, check=True)
    return work, env


def isolated_codex_home():
    temp = tempfile.TemporaryDirectory(prefix="pawl-codex-home-")
    home = Path(temp.name)
    source = Path(os.environ.get("CODEX_HOME", Path.home() / ".codex")) / "auth.json"
    if source.exists():
        shutil.copy2(source, home / "auth.json")
        (home / "auth.json").chmod(0o600)
    return temp, home


def run_arm(codex, work, env, prompt, transcript, model=None):
    command = [
        codex,
        "--ask-for-approval", "never",
        "--sandbox", "workspace-write",
        "--cd", str(work),
        "--config", "shell_environment_policy.inherit=all",
    ]
    if model:
        command += ["--model", model]
    command += ["exec", "--ephemeral", "--ignore-rules", "--json", prompt]
    with transcript.open("w") as stdout, transcript.with_suffix(".stderr").open("w") as stderr:
        return run(command, cwd=work, env=env, stdout=stdout, stderr=stderr).returncode


def score(transcript, work):
    scorer = run([str(HERE / "pawl-invocations.py"), str(transcript)], capture_output=True)
    auditor = run([
        str(HERE / "audit-eval-transcript.py"), str(transcript), str(work), str(REPO)
    ], capture_output=True)
    return {
        "score_exit": scorer.returncode,
        "score": scorer.stdout.strip(),
        "audit_exit": auditor.returncode,
        "audit": auditor.stdout.strip(),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--pawl", required=True, type=Path)
    parser.add_argument("--codex", default=shutil.which("codex") or "codex")
    parser.add_argument("--task", type=int, choices=(5, 6), required=True)
    parser.add_argument("--model")
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    if not args.pawl.is_file():
        parser.error(f"--pawl is not a file: {args.pawl}")
    if args.output.resolve().is_relative_to(REPO):
        parser.error("--output must be outside the pawl repository")
    args.output.mkdir(parents=True, exist_ok=False)

    base_prompt = task_prompt(args.task)
    prompt = (base_prompt + " First run `command -v pawl`; if it is unavailable, stop instead of "
              "substituting another measurement. Work only in this repository.")
    codex_temp, codex_home = isolated_codex_home()
    try:
        results = {"task": args.task, "model": args.model, "arms": {}}
        for arm in ("control", "treatment"):
            work, env = prepare_arm(args.output, arm, args.pawl.resolve())
            env["CODEX_HOME"] = str(codex_home)
            transcript = args.output / f"{arm}.jsonl"
            agent_exit = run_arm(args.codex, work, env, prompt, transcript, args.model)
            results["arms"][arm] = {"agent_exit": agent_exit, **score(transcript, work)}
        summary = args.output / "summary.json"
        summary.write_text(json.dumps(results, indent=2) + "\n")
        print(summary)
        if any(v["agent_exit"] != 0 or v["audit_exit"] != 0 for v in results["arms"].values()):
            raise SystemExit(1)
    finally:
        codex_temp.cleanup()


if __name__ == "__main__":
    main()
