"""Focused product evidence for the normal installed-base plus loose-module PFB."""
from __future__ import annotations

import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import uuid

from scripts.acceptance.content_io_catalog import load_catalog, ROOT
from scripts.acceptance.content_io_product_inputs import read_operator_inputs, source_receipt
from scripts.pfb.docker import app_container_health, app_container_running
from scripts.pfb.identity import compose_project
from scripts.pfb.spec import load_spec


def select_cases(catalog: list[dict], requested: list[str]) -> list[dict]:
    if not requested or len(set(requested)) != len(requested) or not set(requested) <= {row["caseId"] for row in catalog}:
        raise ValueError("CONTENT_IO_PRODUCT_CASE_SELECTION_INVALID")
    return [row for row in catalog if row["caseId"] in requested]


def run_pfb_product(args) -> int:
    from scripts.acceptance.content_io_product_check import execute_case

    spec = load_spec(ROOT)
    if not app_container_running(compose_project(spec["id"])) or app_container_health(compose_project(spec["id"])) != "healthy":
        raise ValueError("CONTENT_IO_PRODUCT_BLOCKED_ENV")
    cases = select_cases(load_catalog(), args.case)
    inputs_path = args.inputs.resolve(strict=True)
    inputs = read_operator_inputs(inputs_path, spec["id"], cases)
    chrome = args.chrome.resolve(strict=True)
    node = shutil.which("node")
    if not node or not chrome.is_file():
        raise ValueError("CONTENT_IO_PRODUCT_BLOCKED_TOOLS")
    run_id = str(uuid.uuid4())
    output = (args.output or ROOT / ".pfb/workspace/content-io/products" / run_id).resolve()
    if not output.is_relative_to(ROOT) or output.exists():
        raise ValueError("CONTENT_IO_PRODUCT_OUTPUT_INVALID")
    output.mkdir(parents=True)
    environment = {"tools": {"node": {"path": node}, "python": {"path": sys.executable}, "chrome": {"path": str(chrome)}},
                   "pfb": {"hostOrigin": f'http://{spec["id"]}.localhost:3000'}}

    def snapshot(name: str) -> dict:
        path = output / name
        command = [node, "scripts/acceptance/content_io_pfb_identity.mjs", "--chrome", str(chrome), "--output", str(path)]
        for case in cases:
            command.extend(["--case", case["caseId"]])
        subprocess.run(command, cwd=ROOT, check=True, timeout=60)
        return json.loads(path.read_text())

    before = snapshot("identity-before.json")
    receipts = {case["caseId"]: [source_receipt(row) for row in inputs["cases"][case["caseId"]]["sources"]] for case in cases}
    report = {"schemaVersion": 1, "runId": run_id, "status": "FAIL", "scope": "SELECTED_PFB_PRODUCT_CASES",
              "selectedCases": [row["caseId"] for row in cases],
              "omittedCases": [row["caseId"] for row in load_catalog() if row not in cases],
              "operatorInputsSha256": hashlib.sha256(inputs_path.read_bytes()).hexdigest(), "cases": []}
    for case in cases:
        sources = receipts[case["caseId"]]
        provider = before["cases"][case["caseId"]]["identities"]
        identity = {**provider["candidate"], "baselineBundleSha256": provider["baseline"]["bundleSha256"],
                    "baselineModuleSha256": provider["baseline"]["moduleSha256"],
                    "browserSha256": before["browser"]["sha256"],
                    "sourceSha256": hashlib.sha256(json.dumps(sources, sort_keys=True, separators=(",", ":")).encode()).hexdigest()}
        record = execute_case(case, environment, inputs, output / case["caseId"], run_id, sources, identity)
        report["cases"].append(record)
        (output / "product-check.json").write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps({"caseId": case["caseId"], "status": record["status"]}), flush=True)
    after = snapshot("identity-after.json")
    report["providerUnchanged"] = before == after
    if before == after and all(row["status"] == "PASS" for row in report["cases"]) and hashlib.sha256(inputs_path.read_bytes()).hexdigest() == report["operatorInputsSha256"]:
        report["status"] = "PASS"
    (output / "product-check.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps({"status": report["status"], "report": str(output / "product-check.json")}))
    return 0 if report["status"] == "PASS" else 1
