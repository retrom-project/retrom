"""Validate the fixed Content I/O product matrix without opening operator inputs."""
from __future__ import annotations

import json
from pathlib import Path
import re
from scripts.acceptance.content_io_cases import TARGET_MODES, required_scenarios

ROOT = Path(__file__).resolve().parents[2]
CATALOG = ROOT / "tests/fixtures/content-io/product-cases.json"
MODES = {"RANGE", "EAGER", "ON_OPEN", "WORKSPACE", "PROJECT"}
CASE_KEYS = {"caseId", "providerId", "targetId", "inputRoles", "sourceIdentity", "fixtureRef", "fixtureAvailability",
             "checkpointSemantics", "requiredScenarios", "existingAcceptanceEntry", "actions", "expectedObservations", "networkPolicy", "performance"}


def strings(value: object) -> bool:
    return isinstance(value, list) and bool(value) and all(isinstance(item, str) and item for item in value) and len(value) == len(set(value))


def require(condition: bool, code: str) -> None:
    if not condition:
        raise ValueError("CONTENT_IO_CATALOG_" + code)


def validate_entry(value: dict, root: Path) -> None:
    require(isinstance(value, dict) and set(value) == {"path", "arguments", "timeoutSeconds"}, "ENTRY_INVALID")
    path = value["path"]
    require(isinstance(path, str) and bool(re.fullmatch(r"scripts/acceptance/[a-z0-9_]+\.(?:mjs|py)", path)), "ENTRY_INVALID")
    require((root / path).is_file() and (root / path).resolve().is_relative_to(root.resolve()), "ENTRY_MISSING")
    require(type(value["timeoutSeconds"]) is int and 1 <= value["timeoutSeconds"] <= 1200, "TIMEOUT_INVALID")
    require(isinstance(value["arguments"], list) and all(isinstance(arg, str) and re.fullmatch(r"[A-Za-z0-9_-]+", arg) for arg in value["arguments"]), "ARGUMENTS_INVALID")


def validate_actions(actions: object, scenarios: list[str]) -> None:
    require(isinstance(actions, list) and bool(actions), "ACTIONS_INVALID")
    names = []
    for action in actions:
        require(isinstance(action, dict) and set(action) == {"scenario", "operation", "waitFor", "maxWaitMs", "assertion", "failureCode"}, "ACTION_INVALID")
        require(all(isinstance(action[key], str) and action[key].strip() for key in ["scenario", "operation", "waitFor", "assertion", "failureCode"]), "ACTION_INVALID")
        require(action["scenario"] in scenarios and type(action["maxWaitMs"]) is int and 0 < action["maxWaitMs"] <= 300000, "ACTION_INVALID")
        require(bool(re.fullmatch(r"CONTENT_IO_[A-Z_]+", action["failureCode"])), "ACTION_INVALID")
        names.append(action["scenario"])
    require(len(set(names)) == len(names) and set(names) == set(scenarios), "ACTION_COVERAGE")


def validate_case(case: dict, root: Path) -> None:
    require(isinstance(case, dict) and set(case) == CASE_KEYS, "CASE_SCHEMA")
    require(isinstance(case["caseId"], str) and bool(re.fullmatch(r"ACC-[A-Z0-9]+-\d{3}", case["caseId"])), "CASE_ID")
    require(case["providerId"] in {"retrom-runtime", "emulatorjs"} and isinstance(case["targetId"], str) and bool(re.fullmatch(r"[a-z0-9-]+", case["targetId"])), "TARGET_INVALID")
    require(case["targetId"] in TARGET_MODES, "TARGET_COVERAGE")
    require(case["providerId"] == ("emulatorjs" if case["targetId"] in {"neocd", "flycast"} else "retrom-runtime"), "PROVIDER_INVALID")
    require(strings(case["inputRoles"]) and strings(case["fixtureRef"]), "INPUTS_INVALID")
    require(all(re.fullmatch(r"(?:owned|operator):[a-z0-9-]+", ref) for ref in case["fixtureRef"]), "PRIVATE_PATH")
    require(case["fixtureAvailability"] in {"OPERATOR_REQUIRED", "AVAILABLE", "MISSING"}, "AVAILABILITY_INVALID")
    require(case["checkpointSemantics"] in {"INSTANT", "GAME_SAVE"}, "CHECKPOINT_INVALID")
    require(case["sourceIdentity"] in [{"kind": kind, "receiptRequired": True} for kind in ["FILE_SHA256", "INDEX_ENTRY"]], "IDENTITY_INVALID")
    scenarios = case["requiredScenarios"]
    require(strings(scenarios) and required_scenarios(case["targetId"]) == set(scenarios), "SCENARIOS_INVALID")
    policy = case["networkPolicy"]
    require(isinstance(policy, dict) and set(policy) == {"mode", "observeEntireBrowserContext", "recordSessionFetchPolicy", "cacheReuse"}, "POLICY_INVALID")
    require(policy["mode"] in MODES and policy["observeEntireBrowserContext"] is True and policy["recordSessionFetchPolicy"] is True and policy["cacheReuse"] == "NEW_LAUNCH_SAME_ORIGIN_PROFILE", "POLICY_INVALID")
    require(policy["mode"] == TARGET_MODES[case["targetId"]], "TARGET_MODE")
    require(case["performance"] == {"repetitions": 5, "states": ["cold", "warm"], "inputReadyRatio": 1.15, "firstFrameRatio": 1.15, "slackMs": 100}, "PERFORMANCE_INVALID")
    require(case["expectedObservations"] == {"firstFrame": "CASE_SPECIFIC_NONEMPTY_SCENE", "inputReady": "GAMEPAD_DIRECTION_AND_CONFIRM_STATE_CHANGE", "checkpointSemantics": case["checkpointSemantics"], "runtimeIdentity": "OBSERVED_ENVELOPE_AND_ASSET_BYTES"}, "OBSERVATIONS_INVALID")
    if case["targetId"].startswith("rpgmaker-"):
        require(case["inputRoles"] == ["game"] and all(ref.startswith("owned:") for ref in case["fixtureRef"]), "RETIRED_RTP_BOUNDARY")
    validate_entry(case["existingAcceptanceEntry"], root)
    validate_actions(case["actions"], scenarios)


def load_catalog(path: Path = CATALOG, root: Path = ROOT) -> list[dict]:
    value = json.loads(path.read_text(encoding="utf-8"))
    require(isinstance(value, dict) and set(value) == {"schemaVersion", "cases"} and type(value["schemaVersion"]) is int and value["schemaVersion"] == 1, "SCHEMA")
    cases = value["cases"]
    require(isinstance(cases, list) and len(cases) == 21, "TARGET_COVERAGE")
    for case in cases:
        validate_case(case, root)
    require(len({case["caseId"] for case in cases}) == len(cases), "DUPLICATE_CASE")
    require(len({(case["providerId"], case["targetId"]) for case in cases}) == len(cases), "DUPLICATE_TARGET")
    require({case["targetId"] for case in cases} == set(TARGET_MODES), "TARGET_COVERAGE")
    return cases
