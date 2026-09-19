"""Independent owned inputs for repeated ACC-RPG-009 runs without database reset."""
from __future__ import annotations
import hashlib
import json
from pathlib import Path
import shutil

from .rpgmaker_run_fixture import inventory, marker_bytes, MARKER

GENERATIONS = ("rpg2000", "rpg2003", "rpgxp", "rpgvx", "rpgvxace")
RECIPE = "OWNED_RPG_POLICY_RUN_MARKER_V1"


def receipt(original: Path, run_id: str) -> dict:
    base = inventory(original)
    if original.name not in GENERATIONS or MARKER in base:
        raise ValueError("RPG_POLICY_BASE_INVALID")
    data = marker_bytes(run_id)
    return {"recipe": RECIPE, "generation": original.name, "runId": run_id,
            "baseFilesSha256": hashlib.sha256(json.dumps(base, sort_keys=True).encode()).hexdigest(),
            "addedFile": {"logicalName": MARKER, "sizeBytes": len(data), "sha256": hashlib.sha256(data).hexdigest()}}


def create(originals: Path, destination: Path, run_id: str) -> list[dict]:
    receipts = [receipt(originals / name, run_id) for name in GENERATIONS]
    destination.mkdir()
    for name in GENERATIONS:
        original, derived = originals / name, destination / name
        shutil.copytree(original, derived)
        (derived / MARKER).write_bytes(marker_bytes(run_id))
        actual = inventory(derived)
        if set(actual) != {*inventory(original), MARKER} or any(
                actual[key] != value for key, value in inventory(original).items()):
            raise ValueError("RPG_POLICY_SOURCE_CHANGED")
    return receipts


def validate_receipts(originals: Path, receipts: object) -> None:
    if not isinstance(receipts, list) or len(receipts) != len(GENERATIONS):
        raise ValueError("RPG_POLICY_RECEIPTS_INVALID")
    for name, value in zip(GENERATIONS, receipts):
        if not isinstance(value, dict) or not isinstance(value.get("runId"), str):
            raise ValueError("RPG_POLICY_RECEIPT_INVALID")
        if value != receipt(originals / name, value["runId"]):
            raise ValueError("RPG_POLICY_RECEIPT_INVALID")
    if len({value["runId"] for value in receipts}) != 1:
        raise ValueError("RPG_POLICY_RUN_ID_MISMATCH")
