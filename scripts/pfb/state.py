"""Per-worktree PFB state with a closed lifecycle vocabulary."""

from __future__ import annotations

from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from .common import atomic_json, load_json
from .errors import PFBError


STATUSES = {
    "INITIALIZED", "VALIDATED", "BUILT", "RUNNING", "STOPPED", "STALE",
    "BUILD_FAILED",
}


def state_path(root: Path) -> Path:
    return root / ".pfb/state.json"


def new_state(pfb_id: str) -> dict[str, Any]:
    return {
        "schemaVersion": 1,
        "pfbId": pfb_id,
        "status": "INITIALIZED",
        "candidateDigest": None,
        "dataCompatibilityDigest": None,
        "updatedAt": _now(),
        "lastError": None,
    }


def load_state(root: Path, pfb_id: str) -> dict[str, Any]:
    path = state_path(root)
    if not path.exists():
        return new_state(pfb_id)
    value = load_json(path)
    expected = {
        "schemaVersion", "pfbId", "status", "candidateDigest",
        "dataCompatibilityDigest", "updatedAt", "lastError",
    }
    if not isinstance(value, dict) or set(value) != expected:
        raise PFBError("PFB_SPEC_INVALID", "state")
    if value["schemaVersion"] != 1 or value["pfbId"] != pfb_id or value["status"] not in STATUSES:
        raise PFBError("PFB_SPEC_INVALID", "state")
    for field in ("candidateDigest", "dataCompatibilityDigest"):
        item = value[field]
        if item is not None and (not isinstance(item, str) or len(item) != 64):
            raise PFBError("PFB_SPEC_INVALID", "state")
    if not isinstance(value["updatedAt"], str):
        raise PFBError("PFB_SPEC_INVALID", "state")
    if value["lastError"] is not None and not isinstance(value["lastError"], str):
        raise PFBError("PFB_SPEC_INVALID", "state")
    return value


def write_state(
    root: Path,
    pfb_id: str,
    status: str,
    *,
    candidate_digest: str | None = None,
    data_digest: str | None = None,
    error: str | None = None,
) -> dict[str, Any]:
    if status not in STATUSES:
        raise PFBError("PFB_SPEC_INVALID", "state-status")
    previous = load_state(root, pfb_id)
    value = {
        "schemaVersion": 1,
        "pfbId": pfb_id,
        "status": status,
        "candidateDigest": candidate_digest if candidate_digest is not None else previous["candidateDigest"],
        "dataCompatibilityDigest": data_digest if data_digest is not None else previous["dataCompatibilityDigest"],
        "updatedAt": _now(),
        "lastError": error,
    }
    atomic_json(state_path(root), value)
    return value


def _now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
