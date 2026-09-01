"""Owner-local PFB registry with serialized atomic updates."""

from __future__ import annotations

import fcntl
import os
from contextlib import contextmanager
from pathlib import Path
from typing import Any, Iterator

from .common import atomic_json, load_json
from .errors import PFBError
from .identity import compose_project, pfb_id, validate_pfb_id


REGISTRY_VERSION = 1
PFB_STATUSES = {"INITIALIZED", "BUILT", "RUNNING", "STALE", "BUILD_FAILED", "STOPPED"}


def state_root() -> Path:
    configured = os.environ.get("XDG_STATE_HOME")
    base = Path(configured) if configured else Path.home() / ".local/state"
    if not base.is_absolute():
        raise PFBError("PFB_SPEC_INVALID", "state-root")
    return base / "retrom-pfb"


def registry_path() -> Path:
    return state_root() / "registry-v1.json"


def empty_registry() -> dict[str, Any]:
    return {
        "schemaVersion": REGISTRY_VERSION,
        "gateway": None,
        "selectedPfbId": None,
        "pfbs": [],
    }


@contextmanager
def locked_registry() -> Iterator[tuple[dict[str, Any], Path]]:
    root = state_root()
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(root, 0o700)
    lock_path = root / "registry-v1.lock"
    with lock_path.open("a+b") as lock:
        os.chmod(lock_path, 0o600)
        fcntl.flock(lock.fileno(), fcntl.LOCK_EX)
        path = registry_path()
        registry = load_json(path, "PFB_SPEC_INVALID") if path.exists() else empty_registry()
        validate_registry(registry)
        yield registry, path


def save_registry(path: Path, registry: dict[str, Any]) -> None:
    validate_registry(registry)
    atomic_json(path, registry)


def register_spec(registry: dict[str, Any], spec: dict[str, Any]) -> None:
    for item in registry["pfbs"]:
        if item["id"] == spec["id"] and (
            item["name"] != spec["name"] or item["retromRoot"] != spec["retrom"]["root"]
        ):
            raise PFBError("PFB_ID_COLLISION")
        if item["retromRoot"] == spec["retrom"]["root"] and item["id"] != spec["id"]:
            raise PFBError("PFB_WORKTREE_INVALID", "already-registered")
    entry = {
        "id": spec["id"], "name": spec["name"], "retromRoot": spec["retrom"]["root"],
        "composeProject": f"retrom-pfb-{spec['id']}", "status": "INITIALIZED",
    }
    registry["pfbs"] = sorted(
        [item for item in registry["pfbs"] if item["id"] != spec["id"]] + [entry],
        key=lambda item: item["id"],
    )


def registry_entry(registry: dict[str, Any], pfb_identifier: str) -> dict[str, Any]:
    for item in registry["pfbs"]:
        if item["id"] == pfb_identifier:
            return item
    raise PFBError("PFB_SPEC_INVALID", "not-registered")


def validate_registry(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != {"schemaVersion", "gateway", "selectedPfbId", "pfbs"}:
        raise PFBError("PFB_SPEC_INVALID", "registry")
    if value["schemaVersion"] != REGISTRY_VERSION or not isinstance(value["pfbs"], list):
        raise PFBError("PFB_SPEC_INVALID", "registry")
    ids = []
    for item in value["pfbs"]:
        if not isinstance(item, dict) or set(item) != {"id", "name", "retromRoot", "composeProject", "status"}:
            raise PFBError("PFB_SPEC_INVALID", "registry-entry")
        if (
            not isinstance(item["id"], str)
            or not isinstance(item["name"], str)
            or not isinstance(item["retromRoot"], str)
            or not Path(item["retromRoot"]).is_absolute()
            or item["id"] != pfb_id(item["name"])
            or item["composeProject"] != compose_project(item["id"])
            or item["status"] not in PFB_STATUSES
        ):
            raise PFBError("PFB_SPEC_INVALID", "registry-entry")
        validate_pfb_id(item["id"])
        ids.append(item["id"])
    if ids != sorted(set(ids)):
        raise PFBError("PFB_SPEC_INVALID", "registry-order")
    if value["selectedPfbId"] is not None and value["selectedPfbId"] not in ids:
        raise PFBError("PFB_SPEC_INVALID", "selected")
    gateway = value["gateway"]
    if gateway is not None:
        fields = {"contractVersion", "configSha256", "subnet", "gatewayIp", "image"}
        if (
            not isinstance(gateway, dict)
            or set(gateway) != fields
            or gateway["contractVersion"] != 1
            or not isinstance(gateway["configSha256"], str)
            or len(gateway["configSha256"]) != 64
            or any(character not in "0123456789abcdef" for character in gateway["configSha256"])
            or not isinstance(gateway["subnet"], str)
            or not isinstance(gateway["gatewayIp"], str)
            or not isinstance(gateway["image"], str)
            or "@sha256:" not in gateway["image"]
        ):
            raise PFBError("PFB_SPEC_INVALID", "gateway")
    return value
