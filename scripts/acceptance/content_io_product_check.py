"""Execute every declared Content I/O product case in the running PFB, retaining fresh evidence."""
from __future__ import annotations

import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import uuid

from scripts.acceptance.content_io_catalog import load_catalog
from scripts.acceptance.content_io_host_check import ROOT, validate_paths, terminate_owned_group
from scripts.acceptance.content_io_product_inputs import read_operator_inputs, source_receipt
from scripts.pfb.docker import app_container_health, app_container_running
from scripts.pfb.identity import compose_project
from scripts.pfb.spec import load_spec


def execute_case(case: dict, environment: dict, inputs: dict, output: Path, run_id: str, receipts: list[dict], identity: dict) -> dict:
    output.mkdir()
    selected = inputs["cases"][case["caseId"]]
    (output / "input-receipts.json").write_text(json.dumps(receipts, indent=2) + "\n")
    (output / "expected-identity.json").write_text(json.dumps(identity, indent=2) + "\n")
    entry = case["existingAcceptanceEntry"]
    executable = environment["tools"]["python" if entry["path"].endswith(".py") else "node"]["path"]
    command = [executable, entry["path"], *entry["arguments"]]
    child_env = {**os.environ, **selected["environment"], "RETROM_ACCEPTANCE_BASE_URL": environment["pfb"]["hostOrigin"],
                 "RETROM_ACCEPTANCE_CASE_DIR": str(output), "RETROM_CHROME_EXECUTABLE": environment["tools"]["chrome"]["path"],
                 "RETROM_ACCEPTANCE_USERNAME": inputs["authentication"]["username"],
                 "RETROM_ACCEPTANCE_PASSWORD": inputs["authentication"]["password"], "RETROM_CONTENT_IO_RUN_ID": run_id}
    child_env.pop("NODE_TEST_CONTEXT", None)
    record = {"caseId": case["caseId"], "command": command, "startedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "exitCode": None, "timedOut": False, "status": "FAIL"}
    with (output / "stdout.log").open("x") as stdout, (output / "stderr.log").open("x") as stderr:
        process = subprocess.Popen(command, cwd=ROOT, env=child_env, stdout=stdout, stderr=stderr, start_new_session=True)
        try:
            record["exitCode"] = process.wait(timeout=entry["timeoutSeconds"])
        except subprocess.TimeoutExpired:
            record["timedOut"] = True
            terminate_owned_group(process)
            record["exitCode"] = process.wait()
        finally:
            terminate_owned_group(process)
    unchanged = receipts == [source_receipt(source) for source in selected["sources"]]
    record["inputUnchanged"] = unchanged
    record["endedAt"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    if record["exitCode"] == 0 and not record["timedOut"] and unchanged:
        validated = subprocess.run([environment["tools"]["node"]["path"], "scripts/acceptance/content_io_product_evidence.mjs",
                                    "--case", case["caseId"], "--run", run_id, "--directory", str(output)],
                                   cwd=ROOT, capture_output=True, text=True, check=False, timeout=30)
        (output / "validation.log").write_text(validated.stdout + validated.stderr)
        if validated.returncode == 0:
            record["status"] = "PASS"
    (output / "command.json").write_text(json.dumps(record, indent=2) + "\n")
    return record


def identities(environment: dict, environment_path: Path, output: Path) -> dict:
    subprocess.run([environment["tools"]["node"]["path"], "scripts/content-io/product-identities.mjs", "--env", str(environment_path),
                    "--output", str(output)], cwd=environment["repositories"]["runtime"]["root"], check=True, timeout=300)
    return json.loads(output.read_text())


def expected_identity(case: dict, snapshot: dict, receipts: list[dict], environment: dict) -> dict:
    provider = next(row for row in snapshot["providers"] if row["id"] == case["providerId"])
    source_bytes = json.dumps(receipts, sort_keys=True, separators=(",", ":")).encode()
    return {"bundleSha256": provider["bundleSha256"], "moduleSha256": provider["moduleSha256"], "workerSha256": provider["workerSha256"],
            "baselineBundleSha256": environment["productionPins"][case["providerId"]]["lock"]["bundleSha256"],
            "sourceSha256": hashlib.sha256(source_bytes).hexdigest(),
            "browserSha256": snapshot["browser"]["sha256"]}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    run_id = str(uuid.uuid4())
    output = args.output or ROOT / ".pfb/workspace/content-io/products" / run_id
    environment = validate_paths(args.env, output)
    spec = load_spec(ROOT)
    if spec["id"] != environment["pfb"]["id"] or spec["runtime"]["root"] != environment["repositories"]["runtime"]["root"]:
        raise ValueError("CONTENT_IO_PRODUCT_PFB_MISMATCH")
    if not environment["tools"]["chrome"] or not app_container_running(compose_project(spec["id"])) or app_container_health(compose_project(spec["id"])) != "healthy":
        raise ValueError("CONTENT_IO_PRODUCT_BLOCKED_ENV")
    cases = load_catalog()
    input_path = ROOT / ".pfb/workspace/content-io/operator-inputs.json"
    inputs = read_operator_inputs(input_path, spec["id"], cases)
    output.mkdir(parents=True, exist_ok=False)
    # Freeze hashes before the first browser opens. Private paths and authentication are never copied.
    before = identities(environment, args.env, output / "identity-before.json")
    receipts = {case["caseId"]: [source_receipt(source) for source in inputs["cases"][case["caseId"]]["sources"]] for case in cases}
    report = {"schemaVersion": 1, "runId": run_id, "status": "FAIL", "environmentSha256": hashlib.sha256(args.env.read_bytes()).hexdigest(),
              "operatorInputsSha256": hashlib.sha256(input_path.read_bytes()).hexdigest(), "cases": []}
    for case in cases:
        sources = receipts[case["caseId"]]
        record = execute_case(case, environment, inputs, output / case["caseId"], run_id, sources, expected_identity(case, before, sources, environment))
        report["cases"].append(record)
        (output / "product-check.json").write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps({"caseId": case["caseId"], "status": record["status"]}), flush=True)
    after = identities(environment, args.env, output / "identity-after.json")
    if before == after and all(row["status"] == "PASS" for row in report["cases"]) and hashlib.sha256(input_path.read_bytes()).hexdigest() == report["operatorInputsSha256"]:
        report["status"] = "PASS"
    (output / "product-check.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps({"status": report["status"], "report": str(output / "product-check.json")}))
    return 0 if report["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
