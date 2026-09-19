#!/usr/bin/env python3
"""Bounded, explicit execution evidence for registered refactoring tests.

The point runner is an in-progress coordinator. It never treats leaf test passes
as completed architecture, operation coverage, or final product acceptance.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import re
import signal
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CASE_FIELDS = {
    "id", "point_id", "tier", "target_file", "symbol", "setup_and_action",
    "required_assertions", "runner", "hard_timeout_seconds", "required",
}
GO_RUNNER_FIELDS = {"type", "command", "must_execute"}
POINT_PATTERN = re.compile(r"RF(?:0[1-9]|1[0-9]|2[0-2])")
GO_TEST_PATTERN = re.compile(r"Test[A-Za-z0-9_]+")
BASELINE = "82834bade1648da3067ebda1b1fc89c18577fd6a"


class AnalysisError(ValueError):
    """Missing, contradictory or unparseable evidence cannot pass."""


def reject_duplicate_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise AnalysisError(f"duplicate registry key: {key}")
        result[key] = value
    return result


def load_registry(root: Path) -> list[dict]:
    registry = json.loads(
        (root / "quality/architecture/test-cases.json").read_text(encoding="utf-8"),
        object_pairs_hook=reject_duplicate_keys,
    )
    if not isinstance(registry, dict) or set(registry) != {"schemaVersion", "baseline", "cases"}:
        raise AnalysisError("unknown or missing test registry fields")
    if type(registry["schemaVersion"]) is not int or registry["schemaVersion"] != 1 or registry["baseline"] != BASELINE:
        raise AnalysisError("test registry schema or baseline mismatch")
    cases = registry["cases"]
    if not isinstance(cases, list) or not cases:
        raise AnalysisError("empty test registry")
    seen: set[str] = set()
    for case in cases:
        validate_case(case)
        if case["id"] in seen:
            raise AnalysisError("duplicate test case identifier")
        seen.add(case["id"])
    return cases


def validate_case(case: dict) -> None:
    if not isinstance(case, dict) or set(case) != CASE_FIELDS:
        raise AnalysisError("unknown or missing test case fields")
    strings = CASE_FIELDS - {"runner", "hard_timeout_seconds", "required"}
    if any(not isinstance(case[key], str) or not case[key].strip() for key in strings):
        raise AnalysisError("test case text fields must be nonempty strings")
    if not POINT_PATTERN.fullmatch(case["point_id"]):
        raise AnalysisError("invalid work item identifier")
    if not re.fullmatch("RFA-" + case["point_id"] + r"-[a-z][a-z0-9_]+", case["id"]):
        raise AnalysisError("case and work item disagree")
    if case["tier"] not in {"tool", "unit", "integration", "browser"}:
        raise AnalysisError("unsupported test tier")
    if case["required"] is not True or type(case["hard_timeout_seconds"]) is not int:
        raise AnalysisError("required case or deadline invalid")
    if not 1 <= case["hard_timeout_seconds"] <= 180:
        raise AnalysisError("case deadline outside the bounded execution contract")
    target = Path(case["target_file"])
    if target.is_absolute() or ".." in target.parts or target.as_posix() != case["target_file"]:
        raise AnalysisError("unsafe test source path")
    if not re.fullmatch(r"[A-Za-z0-9_./-]+", case["target_file"]):
        raise AnalysisError("unsafe test source characters")
    validate_runner(case)


def validate_runner(case: dict) -> None:
    runner = case["runner"]
    if not isinstance(runner, dict):
        raise AnalysisError("test runner must be an object")
    kind = runner.get("type")
    if kind == "go-test":
        if set(runner) != GO_RUNNER_FIELDS or not GO_TEST_PATTERN.fullmatch(case["symbol"]):
            raise AnalysisError("invalid Go runner contract")
        if case["tier"] == "browser" or runner["must_execute"] != case["symbol"]:
            raise AnalysisError("Go runner must execute the registered symbol")
        if runner["command"] != go_test_command(case):
            raise AnalysisError("Go runner command must match its bounded named case")
    elif kind == "vitest":
        if set(runner) != {"type", "command", "test_title"} or runner["test_title"] != case["symbol"]:
            raise AnalysisError("invalid Vitest runner contract")
        if runner["command"] != ["make", "web-test"] or case["tier"] != "unit":
            raise AnalysisError("invalid Vitest leaf metadata")
        if not case["target_file"].startswith("web/") or not case["target_file"].endswith(".test.ts"):
            raise AnalysisError("invalid Vitest test source")
    elif kind == "playwright-leaf":
        expected = {
            "type": kind, "entry": "scripts/refactor_verify.py", "test_file": case["target_file"],
            "test_title": case["symbol"], "isolation": "reuse-existing-Retrom-acceptance-fixture",
            "forbidden_parent_calls": ["refactor-final", "refactor-verify", "acceptance-case-for-own-RFA"],
        }
        if runner != expected or case["tier"] != "browser":
            raise AnalysisError("invalid Playwright leaf contract")
        if not case["target_file"].startswith("web/e2e/") or not case["target_file"].endswith(".spec.ts"):
            raise AnalysisError("invalid Playwright test source")
    else:
        raise AnalysisError("unsupported test runner")


def git_output(root: Path, *arguments: str) -> bytes:
    return subprocess.check_output(["git", *arguments], cwd=root, stderr=subprocess.PIPE)


def source_fingerprint(root: Path) -> dict:
    entries = git_output(root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
    names = sorted({entry.decode("utf-8") for entry in entries.split(b"\0") if entry})
    if not names:
        raise AnalysisError("empty repository snapshot")
    files = []
    for name in names:
        source = root / name
        if source.resolve() != source.absolute() or not source.is_file():
            raise AnalysisError("missing or symlinked source: " + name)
        files.append({"path": name, "sha256": hashlib.sha256(source.read_bytes()).hexdigest()})
    canonical = json.dumps(files, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode()
    return {
        "commit": git_output(root, "rev-parse", "HEAD").decode().strip(),
        "tree": git_output(root, "rev-parse", "HEAD^{tree}").decode().strip(),
        "sourceSha256": hashlib.sha256(canonical).hexdigest(),
        "fileCount": len(files),
    }


def go_test_command(case: dict) -> list[str]:
    target = Path(case["target_file"])
    if not target.name.endswith("_test.go") or not target.as_posix().startswith("internal/"):
        raise AnalysisError("Go case must name an internal test source")
    command = ["go", "test", "-count=1", "-json", f"-timeout={case['hard_timeout_seconds']}s"]
    if case["tier"] == "integration":
        command.append("-tags=integration")
    command.extend(["./" + target.parent.as_posix(), "-run", "^" + case["symbol"] + "$"])
    return command


def parse_go_evidence(output: bytes, symbol: str, timeout_seconds: int) -> dict:
    events = []
    executed = False
    package: str | None = None
    terminal: str | None = None
    duration = 0.0
    for line in output.splitlines():
        try:
            event = json.loads(line, object_pairs_hook=reject_duplicate_keys)
        except (ValueError, UnicodeDecodeError) as error:
            raise AnalysisError("invalid Go JSON event stream") from error
        if not isinstance(event, dict) or not isinstance(event.get("Action"), str):
            raise AnalysisError("invalid Go JSON event")
        if event.get("Test") != symbol or event["Action"] == "output":
            continue
        action = event["Action"]
        event_package = event.get("Package")
        if not isinstance(event_package, str) or not event_package or (package and event_package != package):
            raise AnalysisError("registered test package is missing or inconsistent")
        package = event_package
        # Arbitrary test Output is intentionally discarded: it may hold private
        # paths, cookies or provider responses, unlike execution metadata.
        events.append({key: event[key] for key in ("Action", "Package", "Test", "Elapsed") if key in event})
        if terminal is not None or action not in {"run", "pause", "cont", "pass", "fail", "skip"}:
            raise AnalysisError("invalid registered test event sequence")
        if action == "run":
            if executed:
                raise AnalysisError("duplicate execution of registered test")
            executed = True
        elif not executed:
            raise AnalysisError("registered test terminal event precedes execution")
        if action in {"pass", "fail", "skip"}:
            terminal = action
            elapsed = event.get("Elapsed")
            if type(elapsed) not in {int, float} or not math.isfinite(elapsed) or elapsed < 0:
                raise AnalysisError("invalid registered test duration")
            duration = float(elapsed)
    if not executed or terminal is None:
        raise AnalysisError("registered test did not execute to a terminal result")
    return {
        "status": "PASS" if terminal == "pass" and duration <= timeout_seconds else "FAIL",
        "test": symbol, "package": package, "durationSeconds": duration, "events": events,
    }


def kill_owned_process_group(process: subprocess.Popen) -> None:
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass  # The child may have exited between communicate and cancellation.


def run_bounded(command: list[str], root: Path, timeout_seconds: int) -> tuple[int, bytes, bool]:
    process = subprocess.Popen(
        command, cwd=root, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        start_new_session=True,
    )
    try:
        output, _ = process.communicate(timeout=timeout_seconds)
        return process.returncode, output, False
    except subprocess.TimeoutExpired:
        kill_owned_process_group(process)
        output, _ = process.communicate()
        return process.returncode, output, True
    except BaseException:
        if process.poll() is None:
            kill_owned_process_group(process)
            process.communicate()
        raise


def run_go_leaf(case: dict, root: Path) -> dict:
    if not (root / case["target_file"]).is_file():
        raise AnalysisError("registered test source is absent: " + case["target_file"])
    command = go_test_command(case)
    started = time.time_ns() // 1_000_000
    # Compilation is preparation. Go's own test timer enforces the case deadline;
    # the outer bound also prevents an indefinitely stalled compiler/toolchain.
    code, output, timed_out = run_bounded(command, root, 600)
    finished = time.time_ns() // 1_000_000
    result = parse_go_evidence(output, case["symbol"], case["hard_timeout_seconds"])
    if code != 0 or timed_out:
        result["status"] = "FAIL"
    return {
        "caseId": case["id"], "command": command, "startedAtMs": started, "finishedAtMs": finished,
        "exitCode": code, "timedOut": timed_out, **result,
    }


def run_point(root: Path, point: str, destination: Path) -> int:
    if not POINT_PATTERN.fullmatch(point):
        raise AnalysisError("POINT must be RF01 through RF22")
    cases = [case for case in load_registry(root) if case["point_id"] == point]
    if not cases:
        raise AnalysisError("work item has no registered tests")
    git_output(root, "merge-base", "--is-ancestor", BASELINE, "HEAD")
    before = source_fingerprint(root)
    results = []
    for case in cases:
        if case["runner"]["type"] != "go-test":
            results.append({"caseId": case["id"], "status": "NOT_READY", "reason": "structured frontend leaf runner pending"})
            continue
        try:
            results.append(run_go_leaf(case, root))
        except AnalysisError as error:
            results.append({"caseId": case["id"], "status": "ANALYSIS_ERROR", "reason": str(error)})
    if source_fingerprint(root) != before:
        raise AnalysisError("source or HEAD changed during work item execution")
    report = {
        "schemaVersion": 1, "baseline": BASELINE, "point": point, **before,
        "status": "NOT_READY", "cases": results,
        "tools": {"go": go_tool_version(root), "python": sys.version.split()[0]},
        "pending": ["operation and policy coverage", "direct consumer suites", "static contract and permanent gate completion"],
    }
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return 2 if any(result["status"] == "ANALYSIS_ERROR" for result in results) else 1

def go_tool_version(root: Path) -> str:
    return subprocess.check_output(["go", "env", "GOVERSION"], cwd=root, text=True).strip()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["point", "final"])
    parser.add_argument("--point")
    arguments = parser.parse_args()
    try:
        if arguments.mode == "final":
            if arguments.point is not None:
                raise AnalysisError("final does not accept POINT")
            from refactor_final import run_final
            return run_final(ROOT)
        if arguments.point is None:
            raise AnalysisError("point mode requires POINT")
        version = go_tool_version(ROOT)
        if version != "go1.26.5":
            raise AnalysisError("the repository-pinned Go toolchain is required")
        target = ROOT / ".artifacts/refactor/points" / arguments.point / "result.json"
        return run_point(ROOT, arguments.point, target)
    except (OSError, subprocess.SubprocessError, AnalysisError, ValueError) as error:
        print(str(error).replace(str(ROOT), "<repository>"), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
