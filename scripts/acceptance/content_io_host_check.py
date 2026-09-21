"""Run Host protocol and observer checks in the existing PFB toolchain; never build or start an app."""
from __future__ import annotations

import argparse
import datetime
import json
import os
from pathlib import Path
import signal
import subprocess
import sys

from scripts.pfb.docker import DEV_IMAGE_PREFIX, _require_toolchain, _run_dev_command, workspace_paths
from scripts.pfb.spec import load_spec

ROOT = Path(__file__).resolve().parents[2]
OBSERVER_TESTS = ["content_io_observation", "content_fetch_policy", "content_store_events", "content_io_performance", "content_io_measurement",
                  "content_io_product_evidence", "content_io_case_proof", "content_io_verified_delivery", "indexed_loading_evidence", "play_loading_evidence",
                  "wasm4_fixture", "wasm4_boundary_cart", "fantasy_fixture", "fantasy_run_cart", "fantasy_performance_assets", "ruffle_run_movie"]


def commands(quality: bool = False) -> list[list[str]]:
    if quality:
        return [["make", "quality-structure-check", "fmt-check", "build", "test", "lint-go", "integration-test",
                 "web-build", "NEXT_DIST_DIR=.next-build"]]
    return [
        ["node", "--test", *[f"scripts/acceptance/tests/{name}_test.mjs" for name in OBSERVER_TESTS]],
        ["python3", "-m", "unittest", "scripts.acceptance.tests.test_content_io_catalog",
         "scripts.acceptance.tests.test_content_io_product_inputs", "scripts.acceptance.tests.test_content_io_host_check",
         "scripts.acceptance.tests.test_content_io_product_process",
         "scripts.acceptance.tests.test_rpgmaker_policy_fixture", "scripts.acceptance.tests.test_rpgmaker_resource_policy"],
        ["go", "test", "-tags", "integration", "./internal/httpapi/...", "./internal/launch/...",
         "./internal/platformcatalog/...", "./internal/runtimebundle/...", "./internal/runtimecatalog/...",
         "./internal/runtimelaunch/...", "./internal/runtimeoptions/...", "-count=1"],
        ["make", "web-lint", "web-typecheck", "web-test"],
    ]


def validate_paths(environment: Path, output: Path) -> dict:
    if not environment.is_absolute() or environment.resolve(strict=True) != environment or not output.is_absolute():
        raise ValueError("CONTENT_IO_HOST_PATH_INVALID")
    value = json.loads(environment.read_text())
    evidence = ROOT / ".pfb/workspace/content-io"
    if value["repositories"]["retrom"]["root"] != str(ROOT) or value["paths"]["evidenceRoot"] != str(evidence):
        raise ValueError("CONTENT_IO_HOST_ENVIRONMENT_MISMATCH")
    if not environment.is_relative_to(evidence) or not output.is_relative_to(evidence) or output == evidence:
        raise ValueError("CONTENT_IO_HOST_PATH_INVALID")
    if output.resolve() != output or any(part == ".." for part in output.parts):
        raise ValueError("CONTENT_IO_HOST_PATH_INVALID")
    return value


def terminate_owned_group(process: subprocess.Popen) -> None:
    # Only callers that created this process with start_new_session=True may use this.
    # A child timeout can end the driver while leaving Chrome descendants alive.
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass


def execute(command: list[str], output: Path, index: int, timeout: float = 600) -> dict:
    record = {"command": command, "cwd": str(ROOT), "startedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "exitCode": None, "timedOut": False, "stdout": f"{index}.stdout.log", "stderr": f"{index}.stderr.log"}
    with (output / record["stdout"]).open("x") as stdout, (output / record["stderr"]).open("x") as stderr:
        process = subprocess.Popen(command, cwd=ROOT, stdout=stdout, stderr=stderr, start_new_session=True)
        try:
            record["exitCode"] = process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            record["timedOut"] = True
            terminate_owned_group(process)
            record["exitCode"] = process.wait()
        finally:
            terminate_owned_group(process)
    record["endedAt"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    (output / f"{index}.command.json").write_text(json.dumps(record, indent=2) + "\n")
    return record


def inside(output: Path, quality: bool = False) -> int:
    declaration = ROOT / "web/next-env.d.ts"
    original = declaration.read_bytes() if quality else None
    try:
        return run_commands(output, quality)
    finally:
        if original is not None:
            current = declaration.read_bytes()
            expected = original.replace(b'"./.next/', b'"./.next-build/')
            if current not in (original, expected):
                raise ValueError("CONTENT_IO_HOST_GENERATED_FILE_CHANGED")
            if current != original:
                declaration.write_bytes(original)


def run_commands(output: Path, quality: bool) -> int:
    records = []
    for index, command in enumerate(commands(quality)):
        record = execute(command, output, index)
        records.append(record)
        if record["exitCode"] != 0 or record["timedOut"]:
            break
    passed = len(records) == len(commands(quality)) and all(row["exitCode"] == 0 and not row["timedOut"] for row in records)
    (output / "host-check.json").write_text(json.dumps({"schemaVersion": 1, "status": "PASS" if passed else "FAIL", "commands": records}, indent=2) + "\n")
    return 0 if passed else 1


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--inside", action="store_true", help=argparse.SUPPRESS)
    parser.add_argument("--quality", action="store_true", help="Run final Host quality gates with an isolated Next build directory")
    args = parser.parse_args()
    value = validate_paths(args.env, args.output)
    spec = json.loads((ROOT / ".pfb/spec.json").read_text()) if args.inside else load_spec(ROOT)
    if spec["id"] != value["pfb"]["id"] or spec["name"] != value["pfb"]["name"] or os.getuid() == 0:
        raise ValueError("CONTENT_IO_HOST_PFB_MISMATCH")
    if args.inside:
        return inside(args.output, args.quality)
    _require_toolchain(ROOT, spec)
    args.output.mkdir(parents=True, exist_ok=False)
    paths = workspace_paths(ROOT)
    marker = json.loads((paths["root"] / "toolchain.json").read_text())
    command = ["python3", "-m", "scripts.acceptance.content_io_host_check", "--inside", "--env", str(args.env), "--output", str(args.output)]
    if args.quality:
        command.append("--quality")
    _run_dev_command(ROOT, spec, DEV_IMAGE_PREFIX + ":" + marker["toolchainSha256"], paths, ROOT, command)
    print(json.dumps({"status": "PASS", "report": str(args.output / "host-check.json")}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
