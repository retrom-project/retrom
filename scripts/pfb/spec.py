"""Strict PFB specification handling."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from .common import atomic_json, load_json, strict_object
from .errors import PFBError
from .identity import pfb_id, validate_core_id, validate_pfb_id
from .source_tree import checked_worktree, worktree_identity


HOST_MODE = "LOCALHOST_SHARED_GATEWAY_V1"
SPEC_FIELDS = {"schemaVersion", "name", "id", "hostMode", "retrom", "runtime", "cores"}


def create_spec(
    name: str,
    retrom_root: Path,
    runtime_root: Path | None,
    core_roots_json: str | None,
) -> dict[str, Any]:
    retrom = worktree_identity(retrom_root)
    runtime = {"mode": "formal"}
    if runtime_root is not None:
        runtime_identity = worktree_identity(runtime_root)
        runtime = {
            "mode": "branch", "root": runtime_identity["root"],
            "branch": runtime_identity["branch"],
        }
    cores = _create_cores(core_roots_json)
    if cores and runtime["mode"] != "branch":
        raise PFBError("PFB_RUNTIME_CANDIDATE_REQUIRED")
    value = {
        "schemaVersion": 1,
        "name": name,
        "id": pfb_id(name),
        "hostMode": HOST_MODE,
        "retrom": {"root": retrom["root"], "branch": retrom["branch"]},
        "runtime": runtime,
        "cores": cores,
    }
    validate_spec(value, Path(retrom["root"]))
    return value

def load_spec(root: Path) -> dict[str, Any]:
    path = root / ".pfb/spec.json"
    value = load_json(path)
    return validate_spec(value, root)


def save_spec(root: Path, value: dict[str, Any]) -> None:
    atomic_json(root / ".pfb/spec.json", value)


def validate_spec(value: Any, expected_retrom_root: Path | None = None) -> dict[str, Any]:
    spec = strict_object(value, SPEC_FIELDS, "PFB_SPEC_INVALID")
    if spec["schemaVersion"] != 1 or spec["hostMode"] != HOST_MODE:
        raise PFBError("PFB_HOST_MODE_UNSUPPORTED")
    if not isinstance(spec["name"], str) or pfb_id(spec["name"]) != spec["id"]:
        raise PFBError("PFB_SPEC_INVALID", "identity")
    validate_pfb_id(spec["id"])
    retrom = _validate_branch_root(spec["retrom"])
    if expected_retrom_root is not None and checked_worktree(expected_retrom_root) != Path(retrom["root"]):
        raise PFBError("PFB_WORKTREE_INVALID")
    runtime = spec["runtime"]
    if runtime == {"mode": "formal"}:
        pass
    elif isinstance(runtime, dict) and set(runtime) == {"mode", "root", "branch"} and runtime["mode"] == "branch":
        _validate_branch_root(runtime)
    else:
        raise PFBError("PFB_SPEC_INVALID", "runtime")
    if not isinstance(spec["cores"], list):
        raise PFBError("PFB_SPEC_INVALID", "cores")
    ids: list[str] = []
    for core in spec["cores"]:
        item = strict_object(core, {"id", "mode", "root", "branch"}, "PFB_SPEC_INVALID")
        if item["mode"] != "branch":
            raise PFBError("PFB_SPEC_INVALID", "core-mode")
        ids.append(validate_core_id(item["id"]))
        _validate_branch_root(item)
    if ids != sorted(set(ids), key=lambda item: item.encode("utf-8")):
        raise PFBError("PFB_SPEC_INVALID", "core-order")
    if ids and runtime.get("mode") != "branch":
        raise PFBError("PFB_RUNTIME_CANDIDATE_REQUIRED")
    return spec


def _create_cores(raw: str | None) -> list[dict[str, Any]]:
    if raw is None or raw == "":
        return []
    try:
        mapping = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise PFBError("PFB_SPEC_INVALID", "core-roots") from exc
    if not isinstance(mapping, dict):
        raise PFBError("PFB_SPEC_INVALID", "core-roots")
    result = []
    for identifier in sorted(mapping, key=lambda item: item.encode("utf-8")):
        validate_core_id(identifier)
        root = mapping[identifier]
        if not isinstance(root, str):
            raise PFBError("PFB_SPEC_INVALID", "core-root")
        identity = worktree_identity(Path(root))
        result.append({
            "id": identifier, "mode": "branch", "root": identity["root"],
            "branch": identity["branch"],
        })
    return result


def _validate_branch_root(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) not in (
        {"root", "branch"}, {"mode", "root", "branch"}, {"id", "mode", "root", "branch"},
    ):
        raise PFBError("PFB_SPEC_INVALID", "worktree")
    root = value.get("root")
    branch = value.get("branch")
    if not isinstance(root, str) or not isinstance(branch, str) or not branch:
        raise PFBError("PFB_SPEC_INVALID", "worktree")
    identity = worktree_identity(Path(root))
    if identity["branch"] != branch:
        raise PFBError("PFB_WORKTREE_INVALID", "branch")
    return value
