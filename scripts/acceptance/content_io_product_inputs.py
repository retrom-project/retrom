"""Explicit operator inputs stay outside source control and are hashed without publishing paths."""
from __future__ import annotations

import hashlib
import json
from pathlib import Path
import re


def digest_file(path: Path) -> tuple[str, int]:
    digest = hashlib.sha256()
    size = 0
    with path.open("rb") as source:
        while chunk := source.read(8 * 1024 * 1024):
            digest.update(chunk)
            size += len(chunk)
    return digest.hexdigest(), size


def source_receipt(source: dict) -> dict:
    path = Path(source["path"])
    if not path.is_absolute() or path.resolve(strict=True) != path or path.is_symlink():
        raise ValueError("CONTENT_IO_PRODUCT_SOURCE_PATH_INVALID")
    if path.is_file():
        digest, size = digest_file(path)
        return {"id": source["id"], "role": source["role"], "sha256": digest, "sizeBytes": size, "files": 1}
    entries = []
    for child in sorted(path.rglob("*")):
        if child.is_symlink():
            raise ValueError("CONTENT_IO_PRODUCT_SOURCE_LINK")
        if child.is_file():
            digest, size = digest_file(child)
            entries.append([child.relative_to(path).as_posix(), digest, size])
    if not entries:
        raise ValueError("CONTENT_IO_PRODUCT_SOURCE_EMPTY")
    digest = hashlib.sha256(json.dumps(entries, ensure_ascii=True, separators=(",", ":")).encode()).hexdigest()
    return {"id": source["id"], "role": source["role"], "sha256": digest, "sizeBytes": sum(row[2] for row in entries), "files": len(entries)}


def read_operator_inputs(path: Path, pfb_id: str, cases: list[dict]) -> dict:
    if path.is_symlink() or path.resolve(strict=True) != path:
        raise ValueError("CONTENT_IO_PRODUCT_INPUT_PATH_INVALID")
    value = json.loads(path.read_text())
    if set(value) != {"schemaVersion", "pfbId", "authentication", "cases"} or value["schemaVersion"] != 1 or value["pfbId"] != pfb_id:
        raise ValueError("CONTENT_IO_PRODUCT_INPUT_SCHEMA")
    authentication = value["authentication"]
    if set(authentication) != {"username", "password"} or not all(isinstance(item, str) and item for item in authentication.values()):
        raise ValueError("CONTENT_IO_PRODUCT_AUTH_REQUIRED")
    if set(value["cases"]) != {case["caseId"] for case in cases}:
        raise ValueError("CONTENT_IO_PRODUCT_INPUT_COVERAGE")
    for case in cases:
        validate_case_inputs(value["cases"][case["caseId"]], case)
    return value


def validate_case_inputs(value: dict, case: dict) -> None:
    if set(value) != {"environment", "sources"} or not isinstance(value["environment"], dict) or not isinstance(value["sources"], list):
        raise ValueError("CONTENT_IO_PRODUCT_CASE_INPUT_SCHEMA")
    protected = {"RETROM_ACCEPTANCE_BASE_URL", "RETROM_ACCEPTANCE_CASE_DIR", "RETROM_ACCEPTANCE_USERNAME", "RETROM_ACCEPTANCE_PASSWORD",
                 "RETROM_CHROME_EXECUTABLE", "RETROM_CONTENT_IO_ENV", "RETROM_CONTENT_IO_RUN_ID"}
    for key, entry in value["environment"].items():
        if not re.fullmatch(r"RETROM_[A-Z0-9_]+", key) or key in protected or not isinstance(entry, str) or "\0" in entry:
            raise ValueError("CONTENT_IO_PRODUCT_ENVIRONMENT_INVALID")
    if any(not isinstance(row, dict) or set(row) != {"id", "role", "path"} or not all(isinstance(row[key], str) for key in ["id", "role", "path"]) for row in value["sources"]):
        raise ValueError("CONTENT_IO_PRODUCT_SOURCE_SCHEMA")
    identifiers = [row["id"] for row in value["sources"]]
    keys = [(row["id"], row["role"], row["path"]) for row in value["sources"]]
    if len(keys) != len(set(keys)) or set(identifiers) != set(case["fixtureRef"]) or {row["role"] for row in value["sources"]} != set(case["inputRoles"]):
        raise ValueError("CONTENT_IO_PRODUCT_SOURCE_COVERAGE")
