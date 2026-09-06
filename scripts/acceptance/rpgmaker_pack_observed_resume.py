"""Explicitly finish a complete browser observation after an inspector failure."""
import hashlib
import json
import os
import re
import subprocess
import time
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
BROWSER_INPUTS = ("scripts/acceptance/rpgmaker_pack.mjs", "scripts/acceptance/rpgmaker_pack_population.mjs",
                  "scripts/acceptance/rpgmaker_url.mjs", "web/package-lock.json")
SHA = re.compile(r"[0-9a-f]{64}")


def require(value):
    if not value:
        raise ValueError("RPG_ACCEPTANCE_PACK_OBSERVED_RESUME_INVALID")


def validate_source(request, result, observed):
    require(isinstance(request, dict) and set(request) == {
        "schemaVersion", "attempt", "resultSha256", "observationSha256", "provisionSha256", "screenshots"})
    require(request["schemaVersion"] == 1 and re.fullmatch(r"[0-9]{3}", str(request["attempt"])) and
            all(SHA.fullmatch(str(request[key])) for key in ("resultSha256", "observationSha256", "provisionSha256")))
    require(isinstance(observed, dict) and observed.get("schemaVersion") == 1 and
            observed.get("caseId") == "ACC-RPG-009" and observed.get("status") == "OBSERVED" and
            "inspectionResume" not in observed and "databaseEvidence" not in observed)
    require(isinstance(result, dict) and result.get("caseId") == "ACC-RPG-009" and result.get("status") == "FAIL" and
            result.get("timedOut") is False and result.get("productEvidence") == observed and
            all(type(result.get(key)) is int for key in ("startedAtMs", "finishedAtMs", "durationMs")) and
            0 < result["durationMs"] == result["finishedAtMs"] - result["startedAtMs"] <= 300_000)
    require(isinstance(request["screenshots"], dict) and request["screenshots"] and
            set(request["screenshots"]) == set(observed.get("screenshots", [])) and
            all(re.fullmatch(r"screenshots/[a-z0-9-]+\.png", name) and SHA.fullmatch(str(digest))
                for name, digest in request["screenshots"].items()))


def checked_bytes(path, digest=None):
    require(path.is_absolute() and path.is_file() and not path.is_symlink() and path.resolve() == path)
    data = path.read_bytes()
    require(digest is None or hashlib.sha256(data).hexdigest() == digest)
    return data


def prepare_observed_resume(case_dir):
    value = os.environ.get("RETROM_ACC_RPG_009_OBSERVED_RESUME")
    if not value:
        return None
    request = json.loads(checked_bytes(Path(value)))
    require(isinstance(request, dict) and re.fullmatch(r"[0-9]{3}", str(request.get("attempt"))))
    original = case_dir.resolve() / "attempts" / request["attempt"]
    result_path, observed_path = original / "result.json", original / "rpgmaker-product.json"
    result = json.loads(checked_bytes(result_path, request.get("resultSha256")))
    observed_bytes = checked_bytes(observed_path, request.get("observationSha256"))
    observed = json.loads(observed_bytes)
    validate_source(request, result, observed)
    require(checked_bytes(original / "stdout.log").decode().strip() == "RPG_ACCEPTANCE_PACK_DATABASE_INSPECT_FAILED")
    checked_bytes(Path(os.environ["RETROM_ACC_RPG_009_PROVISION_EVIDENCE"]), request["provisionSha256"])
    verify_browser_inputs(result)
    images = {name: checked_bytes(original / name, digest) for name, digest in request["screenshots"].items()}
    # The prior files remain immutable; the new Case has its own observed copy.
    with (case_dir / "rpgmaker-product.json").open("xb") as output:
        output.write(observed_bytes)
    for name, data in images.items():
        with (case_dir / name).open("xb") as output:
            output.write(data)
    return {"schemaVersion": 1, "mode": "EXPLICIT_OBSERVED_REINSPECTION", "attempt": request["attempt"],
            "sourceResult": result_path.relative_to(case_dir.parents[1]).as_posix(),
            "sourceResultSha256": request["resultSha256"], "observationSha256": request["observationSha256"],
            "provisionSha256": request["provisionSha256"], "screenshots": request["screenshots"],
            "sourceStartedAtMs": result["startedAtMs"], "sourceFinishedAtMs": result["finishedAtMs"],
            "sourceDurationMs": result["durationMs"], "inspectionStartedAtMs": int(time.time() * 1000)}


def verify_browser_inputs(result):
    commit = result.get("gitCommit")
    require(isinstance(commit, str) and re.fullmatch(r"[0-9a-f]{40}", commit))
    dirty = result.get("gitDirtySummary", {}).get("entries")
    require(isinstance(dirty, list) and not any(row.get("path") in BROWSER_INPUTS for row in dirty))
    for name in BROWSER_INPUTS:
        source = subprocess.run(["git", "show", f"{commit}:{name}"], cwd=ROOT, capture_output=True, check=False)
        require(source.returncode == 0 and source.stdout == (ROOT / name).read_bytes())


def finish_inspection(metadata, now_ms=None):
    finished = int(time.time() * 1000) if now_ms is None else now_ms
    if finished - metadata["inspectionStartedAtMs"] + metadata["sourceDurationMs"] > 300_000:
        raise ValueError("RPG_ACCEPTANCE_PACK_OBSERVED_RESUME_TIMEOUT")
    return {**metadata, "inspectionFinishedAtMs": finished}


def validate_inspection_resume(value):
    require(isinstance(value, dict) and set(value) == {
        "schemaVersion", "mode", "attempt", "sourceResult", "sourceResultSha256", "observationSha256",
        "provisionSha256", "screenshots", "sourceStartedAtMs", "sourceFinishedAtMs", "sourceDurationMs",
        "inspectionStartedAtMs", "inspectionFinishedAtMs"})
    require(value["schemaVersion"] == 1 and value["mode"] == "EXPLICIT_OBSERVED_REINSPECTION" and
            re.fullmatch(r"[0-9]{3}", str(value["attempt"])) and
            value["sourceResult"] == f'cases/acc-rpg-009/attempts/{value["attempt"]}/result.json' and
            all(SHA.fullmatch(str(value[key])) for key in ("sourceResultSha256", "observationSha256", "provisionSha256")))
    require(all(type(value[key]) is int for key in (
        "sourceStartedAtMs", "sourceFinishedAtMs", "sourceDurationMs", "inspectionStartedAtMs", "inspectionFinishedAtMs")))
    require(0 < value["sourceDurationMs"] == value["sourceFinishedAtMs"] - value["sourceStartedAtMs"] and
            value["sourceFinishedAtMs"] < value["inspectionStartedAtMs"] <= value["inspectionFinishedAtMs"])
    require(isinstance(value["screenshots"], dict) and set(value["screenshots"]) == {
        "screenshots/rpgmaker-pack-catalog.png", "screenshots/rpgmaker-pack-review-binding.png"} and
        all(SHA.fullmatch(str(digest)) for digest in value["screenshots"].values()))
    finish_inspection(value, value["inspectionFinishedAtMs"])
