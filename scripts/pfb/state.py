"""Per-worktree PFB state independent from source and provider digests."""

from __future__ import annotations

from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from .common import atomic_json, load_json
from .errors import PFBError

STATUSES = {"INITIALIZED", "READY", "RUNNING", "STOPPED", "ERROR"}


def state_path(root: Path) -> Path:
    return root / ".pfb/state.json"


def new_state(pfb_id: str) -> dict[str, Any]:
    return {"schemaVersion": 2, "pfbId": pfb_id, "status": "INITIALIZED", "updatedAt": _now(), "lastError": None}


def load_state(root: Path, pfb_id: str) -> dict[str, Any]:
    path = state_path(root)
    if not path.exists():
        return new_state(pfb_id)
    value = load_json(path)
    if isinstance(value, dict) and value.get("schemaVersion") == 1 and value.get("pfbId") == pfb_id:
        legacy_status = value.get("status")
        status = "RUNNING" if legacy_status == "RUNNING" else "STOPPED" if legacy_status == "STOPPED" else "INITIALIZED"
        return {"schemaVersion": 2, "pfbId": pfb_id, "status": status,
                "updatedAt": value.get("updatedAt", _now()), "lastError": value.get("lastError")}
    expected = {"schemaVersion", "pfbId", "status", "updatedAt", "lastError"}
    if not isinstance(value, dict) or set(value) != expected or value.get("schemaVersion") != 2 or \
            value.get("pfbId") != pfb_id or value.get("status") not in STATUSES or \
            not isinstance(value.get("updatedAt"), str) or \
            value.get("lastError") is not None and not isinstance(value.get("lastError"), str):
        raise PFBError("PFB_SPEC_INVALID", "state")
    return value


def write_state(root: Path, pfb_id: str, status: str, *, error: str | None = None) -> dict[str, Any]:
    if status not in STATUSES:
        raise PFBError("PFB_SPEC_INVALID", "state-status")
    value = {"schemaVersion": 2, "pfbId": pfb_id, "status": status, "updatedAt": _now(), "lastError": error}
    atomic_json(state_path(root), value)
    return value


def _now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
