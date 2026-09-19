#!/usr/bin/env python3
"""Serial final coordination; static gates never consume this execution report."""

from __future__ import annotations

import fcntl
import hashlib
import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path

import refactor_verify as verify

CONDITIONAL_CASES = {"ACC-NET-002", "ACC-DAT-006"}
POINTS = tuple(f"RF{number:02d}" for number in range(1, 23))
SHA256 = re.compile(r"[0-9a-f]{64}")


def step(identifier: str, command: list[str], timeout: int, kind: str = "command") -> dict:
    return {"id": identifier, "command": command, "timeoutSeconds": timeout, "kind": kind}


def acceptance_catalog(root: Path) -> list[str]:
    document = (root / "docs/project-acceptance.md").read_text(encoding="utf-8")
    cases = re.findall(r"^### (ACC-[A-Z][A-Z0-9]*-\d{3})[：:]", document, re.MULTILINE)
    if not cases or len(cases) != len(set(cases)):
        raise verify.AnalysisError("empty or duplicate original acceptance catalog")
    # ACC-RFA aliases belong to point execution, never to the old product loop.
    aliases = [case for case in cases if case.startswith("ACC-RFA-")]
    if aliases and set(aliases) != {f"ACC-RFA-{number:03d}" for number in range(1, 23)}:
        raise verify.AnalysisError("incomplete refactor acceptance aliases")
    return [case for case in cases if case not in aliases]


def final_plan(root: Path) -> list[dict]:
    registry = verify.load_registry(root)
    if {case["point_id"] for case in registry} != set(POINTS):
        raise verify.AnalysisError("final requires every work item")
    plan = [
        step("preparation", ["make", "install-deps", "prepare-e2e-browser", "deps-check"], 3600),
        step("checker-selftest", ["make", "architecture-selftest"], 1800),
        step("go-architecture", ["make", "architecture-check"], 1200, "go-architecture"),
        step("web-architecture", ["make", "web-architecture-check"], 1200, "web-architecture"),
        step("contracts", ["make", "refactor-contract-check"], 1200, "contracts"),
        step("ci", ["make", "ci"], 7200),
        step("race", ["make", "refactor-race"], 7200),
    ]
    plan.extend(step(point, ["make", "refactor-verify", "POINT=" + point], 3600, "point") for point in POINTS)
    plan.extend([
        step("public-fixtures", ["make", "public-fixtures-check"], 600),
        step("dependencies", ["make", "deps-check"], 600),
        step("web-e2e", ["make", "web-e2e"], 7200),
        step("acceptance-prepare", ["make", "acceptance-prepare"], 600, "acceptance-prepare"),
    ])
    plan.extend(step(case, ["make", "acceptance-case", "CASE=" + case], 7200, "acceptance")
                for case in acceptance_catalog(root))
    plan.append(step("acceptance-report", ["make", "acceptance-report"], 600, "acceptance-report"))
    if len({item["id"] for item in plan}) != len(plan):
        raise verify.AnalysisError("duplicate final step")
    return plan


def require_clean(root: Path) -> None:
    if verify.git_output(root, "status", "--porcelain=v1", "--untracked-files=all").strip():
        raise verify.AnalysisError("final requires a clean candidate worktree")
    verify.git_output(root, "merge-base", "--is-ancestor", verify.BASELINE, "HEAD")


def environment_preflight(root: Path) -> dict:
    if sys.platform != "linux" or os.geteuid() == 0 or os.environ.get("SUDO_UID"):
        raise verify.AnalysisError("final requires the ordinary Linux development user")
    version = verify.go_tool_version(root)
    if version != "go1.26.5":
        raise verify.AnalysisError("final requires the pinned Go toolchain")
    return {"class": "isolated-linux-development", "go": version, "python": sys.version.split()[0]}


def prepared_tool_versions(root: Path) -> dict:
    node = root / ".cache/tools/node-v24.18.0-linux-x64/bin/node"
    chrome = Path(os.environ.get("RETROM_CHROME_EXECUTABLE", root / ".cache/tools/retrom-chrome-for-testing"))
    versions = {}
    for name, command in (("node", [str(node), "--version"]), ("chrome", [str(chrome), "--version"])):
        code, output, timed_out = verify.run_bounded(command, root, 10)
        value = output.decode("utf-8").strip()
        pattern = r"v24\.18\.0" if name == "node" else r"(?:Google Chrome for Testing|Chromium|Google Chrome) [0-9.]+"
        if code != 0 or timed_out or not re.fullmatch(pattern, value):
            raise verify.AnalysisError("prepared tool version invalid: " + name)
        versions[name] = value
    return versions


def read_artifact(root: Path, relative: str, started_ns: int, finished_ns: int) -> tuple[dict, dict]:
    path = root / relative
    if Path(relative).is_absolute() or ".." in Path(relative).parts or path.resolve() != path.absolute():
        raise verify.AnalysisError("unsafe final artifact path")
    if not path.is_file() or not started_ns <= path.stat().st_mtime_ns <= finished_ns:
        raise verify.AnalysisError("missing or stale final artifact: " + relative)
    data = path.read_bytes()
    payload = json.loads(data, object_pairs_hook=verify.reject_duplicate_keys)
    if not isinstance(payload, dict):
        raise verify.AnalysisError("artifact must be a JSON object")
    return payload, {"path": relative, "sha256": hashlib.sha256(data).hexdigest()}


def verify_provenance(payload: dict, snapshot: dict, nested: bool = False) -> None:
    provenance = payload.get("baseline") if nested else payload
    if not isinstance(provenance, dict) or any(provenance.get(key) != snapshot[key] for key in ("commit", "tree")):
        raise verify.AnalysisError("artifact belongs to another candidate")
    if not isinstance(payload.get("sourceSha256"), str) or not SHA256.fullmatch(payload["sourceSha256"]):
        raise verify.AnalysisError("artifact has no source fingerprint")


def verify_point(payload: dict, item: dict, registry: list[dict], snapshot: dict) -> None:
    verify_provenance(payload, snapshot)
    if payload.get("sourceSha256") != snapshot["sourceSha256"] or payload.get("point") != item["id"]:
        raise verify.AnalysisError("point source fingerprint or identity mismatch")
    expected = {case["id"]: case for case in registry if case["point_id"] == item["id"]}
    cases = payload.get("cases")
    if not isinstance(cases, list) or len(cases) != len(expected):
        raise verify.AnalysisError("point evidence does not cover every registered test")
    seen = set()
    for case in cases:
        if not isinstance(case, dict) or case.get("caseId") not in expected or case["caseId"] in seen:
            raise verify.AnalysisError("unknown or duplicate point case")
        seen.add(case["caseId"])
        definition = expected[case["caseId"]]
        if case.get("status") != "PASS":
            raise verify.AnalysisError("point contains a required non-passing case")
        if definition["runner"]["type"] == "go-test":
            evidence = b"\n".join(json.dumps(event).encode() for event in case.get("events", []))
            actual = verify.parse_go_evidence(evidence, definition["symbol"], definition["hard_timeout_seconds"])
            if actual["status"] != "PASS" or case.get("exitCode") != 0 or case.get("timedOut") is not False:
                raise verify.AnalysisError("point has no successful named test execution")
        else:
            raise verify.AnalysisError("structured frontend execution evidence is not implemented")


def acceptance_root(root: Path) -> tuple[str, str]:
    pointer = root / ".artifacts/acceptance/current-run"
    if pointer.resolve() != pointer.absolute():
        raise verify.AnalysisError("symlinked acceptance pointer")
    run_id = pointer.read_text(encoding="utf-8").strip()
    if not re.fullmatch(r"\d{8}T\d{6}Z-[0-9a-f]{8}", run_id):
        raise verify.AnalysisError("invalid acceptance run identity")
    return run_id, ".artifacts/acceptance/" + run_id


def collect_evidence(root: Path, item: dict, result: dict, snapshot: dict, registry: list[dict],
                     context: dict, started_ns: int, finished_ns: int) -> str:
    kind = item["kind"]
    if kind == "command":
        passed = result["exitCode"] == 0 and not result["timedOut"]
        if item["id"] == "preparation" and passed:
            result["tools"] = prepared_tool_versions(root)
        return "PASS" if passed else "FAIL"
    relative = {
        "go-architecture": ".artifacts/refactor/architecture.json",
        "web-architecture": ".artifacts/refactor/web-architecture.json",
        "contracts": ".artifacts/refactor/contract.json",
        "point": ".artifacts/refactor/points/" + item["id"] + "/result.json",
    }.get(kind)
    if kind.startswith("acceptance"):
        run_id, base = acceptance_root(root)
        if kind == "acceptance-prepare":
            context["acceptanceRun"] = run_id
            relative = base + "/run.json"
        else:
            if context.get("acceptanceRun") != run_id:
                raise verify.AnalysisError("acceptance run changed during final execution")
            relative = base + ("/cases/" + item["id"].lower() + "/result.json" if kind == "acceptance" else "/report.json")
    if relative is None:
        raise verify.AnalysisError("unknown evidence kind")
    payload, descriptor = read_artifact(root, relative, started_ns, finished_ns)
    result["artifacts"] = [descriptor]
    status = payload.get("status")
    if kind in {"go-architecture", "web-architecture", "contracts"}:
        verify_provenance(payload, snapshot, nested=kind != "web-architecture")
        if status == "VERIFIED" and (payload.get("violations") != [] or payload.get("pendingChecks") != []):
            raise verify.AnalysisError("static gate claims verification with unresolved findings")
        expected = "VERIFIED"
    elif kind == "point":
        if status == "PASS":
            verify_point(payload, item, registry, snapshot)
            if payload.get("pending") != []:
                raise verify.AnalysisError("point claims PASS with pending work")
        expected = "PASS"
    elif kind in {"acceptance", "acceptance-prepare"}:
        if payload.get("gitCommit") != snapshot["commit"] or payload.get("gitDirty") is not False:
            raise verify.AnalysisError("acceptance evidence belongs to another candidate")
        expected = "PREPARED" if kind == "acceptance-prepare" else "PASS"
        if kind == "acceptance":
            if payload.get("caseId") != item["id"]:
                raise verify.AnalysisError("acceptance case identity mismatch")
            if status == "NOT_APPLICABLE" and item["id"] in CONDITIONAL_CASES:
                expected = "NOT_APPLICABLE"
            elif status == "BLOCKED" and result["exitCode"] != 0 and not result["timedOut"]:
                return "BLOCKED"
    else:
        expected = "PASS"
        if payload.get("runId") != context.get("acceptanceRun"):
            raise verify.AnalysisError("acceptance summary identity mismatch")
        for field in ("failedCaseIds", "blockedCaseIds", "missingCaseIds", "unresolvedDefectIds"):
            if status == "PASS" and payload.get(field) != []:
                raise verify.AnalysisError("acceptance summary claims PASS with unresolved work")
    if status == expected and result["exitCode"] == 0 and not result["timedOut"]:
        return "NOT_APPLICABLE" if status == "NOT_APPLICABLE" else "PASS"
    return "NOT_READY" if status == "NOT_READY" else "FAIL"


def execute_step(root: Path, item: dict, snapshot: dict, registry: list[dict], context: dict) -> dict:
    started_ns = time.time_ns()
    code, output, timed_out = verify.run_bounded(item["command"], root, item["timeoutSeconds"])
    finished_ns = time.time_ns()
    result = {
        "id": item["id"], "command": item["command"], "startedAtMs": started_ns // 1_000_000,
        "finishedAtMs": finished_ns // 1_000_000, "exitCode": code, "timedOut": timed_out,
        # Child stdout can contain private paths or credentials; retain only its digest.
        "outputSha256": hashlib.sha256(output).hexdigest(), "artifacts": [],
    }
    try:
        result["status"] = collect_evidence(root, item, result, snapshot, registry, context, started_ns, finished_ns)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        result.update(status="ANALYSIS_ERROR", reason=safe_reason(error, root))
    return result


def safe_reason(error: Exception, root: Path) -> str:
    if isinstance(error, OSError):
        return "evidence I/O error (" + type(error).__name__ + ")"
    return str(error).replace(str(root), "<repository>")


def run_plan(root: Path, plan: list[dict], snapshot: dict, registry: list[dict]) -> list[dict]:
    context: dict = {}
    results = []
    stopped = False
    for item in plan:
        if stopped:
            results.append({"id": item["id"], "command": item["command"], "status": "NOT_RUN", "reason": "earlier required gate did not pass"})
            continue
        print("refactor-final: " + item["id"], flush=True)
        result = execute_step(root, item, snapshot, registry, context)
        results.append(result)
        if verify.source_fingerprint(root) != snapshot:
            result.update(status="ANALYSIS_ERROR", reason="source or HEAD changed during final execution")
            stopped = True
        elif result["status"] not in {"PASS", "NOT_APPLICABLE"}:
            # Missing legal input does not suppress unrelated original product cases.
            stopped = item["kind"] != "acceptance" or result["status"] == "ANALYSIS_ERROR"
    return results


def final_status(results: list[dict]) -> str:
    statuses = {item["status"] for item in results}
    if not results:
        return "ANALYSIS_ERROR"
    for status in ("ANALYSIS_ERROR", "FAIL", "BLOCKED", "NOT_READY", "NOT_RUN"):
        if status in statuses:
            return status
    return "PASS" if statuses <= {"PASS", "NOT_APPLICABLE"} else "ANALYSIS_ERROR"


def write_report(root: Path, report: dict) -> None:
    destination = root / ".artifacts/refactor/final/result.json"
    if destination.resolve() != destination.absolute():
        raise verify.AnalysisError("symlinked final destination")
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_suffix(".tmp")
    if temporary.exists() or temporary.is_symlink():
        raise verify.AnalysisError("unfinished final report write already exists")
    with temporary.open("x", encoding="utf-8") as output:
        output.write(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    temporary.replace(destination)


def run_final_locked(root: Path) -> int:
    report = {"schemaVersion": 1, "baseline": verify.BASELINE, "startedAtMs": time.time_ns() // 1_000_000, "steps": []}
    try:
        require_clean(root)
        report.update(verify.source_fingerprint(root))
        report["environment"] = environment_preflight(root)
        plan = final_plan(root)
        registry = verify.load_registry(root)
        snapshot = {key: report[key] for key in ("commit", "tree", "sourceSha256", "fileCount")}
        report["steps"] = run_plan(root, plan, snapshot, registry)
        report["status"] = final_status(report["steps"])
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        report.update(status="ANALYSIS_ERROR", reason=safe_reason(error, root))
    report["finishedAtMs"] = time.time_ns() // 1_000_000
    write_report(root, report)
    print("refactor-final: " + report["status"] + " (.artifacts/refactor/final/result.json)")
    return 0 if report["status"] == "PASS" else (2 if report["status"] == "ANALYSIS_ERROR" else 1)


def run_final(root: Path) -> int:
    lock = root / ".artifacts/refactor/final/coordinator.lock"
    if lock.resolve() != lock.absolute():
        raise verify.AnalysisError("symlinked final lock")
    lock.parent.mkdir(parents=True, exist_ok=True)
    with lock.open("a") as handle:
        try:
            fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise verify.AnalysisError("another final coordinator is already running") from error
        return run_final_locked(root)
