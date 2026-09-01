"""Candidate and data-generation lock construction and validation."""

from __future__ import annotations

from pathlib import Path
from typing import Any

from .common import atomic_json, canonical_bytes, load_json, lowercase_hex, sha256_bytes, sha256_file
from .errors import PFBError
from .source_tree import migration_tree_sha256, worktree_identity


def build_lock(root: Path, spec: dict[str, Any]) -> tuple[dict[str, Any], dict[str, Any]]:
    retrom = _lock_identity(worktree_identity(root))
    runtime: dict[str, Any] = {"mode": "formal"}
    if spec["runtime"]["mode"] == "branch":
        runtime = _lock_identity(worktree_identity(Path(spec["runtime"]["root"])))
        runtime["mode"] = "branch"
        descriptor = root / ".pfb/candidates/runtime/retrom-runtime-candidate.json"
        runtime["candidateSha256"] = sha256_file(descriptor) if descriptor.exists() else None
    formal_manifest = root / "data/dat/rpgmaker/v1/manifest.json"
    overlay = root / ".pfb/candidates/runtime/.retrom-pfb-candidate.json"
    candidate_files_sha = _candidate_files_digest(root / ".pfb/candidates")
    lock = {
        "schemaVersion": 1,
        "kind": "RETROM_PFB_LOCK_V1",
        "pfbId": spec["id"],
        "retrom": retrom,
        "runtime": runtime,
        "cores": _core_lock_entries(root, spec),
        "formalDependencyManifestSha256": sha256_file(formal_manifest),
        "runtimeOverlaySha256": sha256_file(overlay) if overlay.exists() else None,
        "candidateFilesSha256": candidate_files_sha,
    }
    candidate_digest = _dependency_candidate_digest(lock)
    migration_digest = migration_tree_sha256(root)
    compatibility_payload = {
        "candidateDigest": candidate_digest,
        "migrationTreeSha256": migration_digest,
        "formalDependencyManifestSha256": lock["formalDependencyManifestSha256"],
    }
    data_digest = sha256_bytes(canonical_bytes(compatibility_payload))
    data_lock = {
        "schemaVersion": 1,
        "kind": "RETROM_PFB_DATA_LOCK_V1",
        "pfbId": spec["id"],
        "candidateDigest": candidate_digest,
        "migrationTreeSha256": migration_digest,
        "dataCompatibilityDigest": data_digest,
    }
    return lock, data_lock


def publish_locks(root: Path, lock: dict[str, Any], data_lock: dict[str, Any]) -> None:
    atomic_json(root / ".pfb/locks/candidate-lock.json", lock)
    atomic_json(root / ".pfb/locks/data-lock.json", data_lock)


def current_locks(root: Path, spec: dict[str, Any]) -> tuple[dict[str, Any], dict[str, Any], str, str]:
    lock = load_json(root / ".pfb/locks/candidate-lock.json")
    data_lock = load_json(root / ".pfb/locks/data-lock.json")
    _validate_lock_shape(lock, data_lock, spec["id"])
    current, current_data = build_lock(root, spec)
    if canonical_bytes(current) != canonical_bytes(lock) or canonical_bytes(current_data) != canonical_bytes(data_lock):
        raise PFBError("PFB_SOURCE_STALE")
    return lock, data_lock, sha256_bytes(canonical_bytes(lock)), data_lock["dataCompatibilityDigest"]


def entrypoint_locks(root: Path, spec: dict[str, Any], runtime_root: Path) -> tuple[str, str]:
    """Validate locked bytes from container paths without trusting host path strings."""
    lock = load_json(root / ".pfb/locks/candidate-lock.json")
    data_lock = load_json(root / ".pfb/locks/data-lock.json")
    _validate_lock_shape(lock, data_lock, spec["id"])
    if lock["retrom"] != _lock_identity(worktree_identity(root)):
        raise PFBError("PFB_SOURCE_STALE", "retrom")
    if spec["runtime"]["mode"] == "formal":
        if lock["runtime"] != {"mode": "formal"}:
            raise PFBError("PFB_SOURCE_STALE", "runtime")
    else:
        actual_runtime = _lock_identity(worktree_identity(runtime_root))
        descriptor = root / ".pfb/candidates/runtime/retrom-runtime-candidate.json"
        actual_runtime.update({"mode": "branch", "candidateSha256": sha256_file(descriptor)})
        if lock["runtime"] != actual_runtime:
            raise PFBError("PFB_SOURCE_STALE", "runtime")
    for core in lock["cores"]:
        descriptor = root / ".pfb/candidates/cores" / core["id"] / "retrom-core-candidate.json"
        if sha256_file(descriptor) != core["candidateSha256"]:
            raise PFBError("PFB_SOURCE_STALE", f"core-{core['id']}")
    manifest_sha = sha256_file(root / "data/dat/rpgmaker/v1/manifest.json")
    overlay = root / ".pfb/candidates/runtime/.retrom-pfb-candidate.json"
    overlay_sha = sha256_file(overlay) if overlay.exists() else None
    if (
        lock["formalDependencyManifestSha256"] != manifest_sha
        or lock["runtimeOverlaySha256"] != overlay_sha
        or lock["candidateFilesSha256"] != _candidate_files_digest(root / ".pfb/candidates")
    ):
        raise PFBError("PFB_SOURCE_STALE", "candidate")
    lock_digest = sha256_bytes(canonical_bytes(lock))
    candidate_digest = _dependency_candidate_digest(lock)
    if data_lock["candidateDigest"] != candidate_digest or data_lock["migrationTreeSha256"] != migration_tree_sha256(root):
        raise PFBError("PFB_DATA_GENERATION_MISMATCH")
    expected_data_digest = sha256_bytes(canonical_bytes({
        "candidateDigest": candidate_digest,
        "migrationTreeSha256": data_lock["migrationTreeSha256"],
        "formalDependencyManifestSha256": manifest_sha,
    }))
    if data_lock["dataCompatibilityDigest"] != expected_data_digest:
        raise PFBError("PFB_DATA_GENERATION_MISMATCH")
    return lock_digest, expected_data_digest


def _lock_identity(value: dict[str, Any]) -> dict[str, Any]:
    return {field: value[field] for field in ("branch", "commit", "dirty", "sourceTreeSha256")}


def _candidate_files_digest(root: Path) -> str:
    files = []
    if root.exists():
        for target in sorted(root.rglob("*")):
            if target.is_file() and not target.is_symlink():
                files.append({
                    "path": target.relative_to(root).as_posix(),
                    "sha256": sha256_file(target),
                })
    return sha256_bytes(canonical_bytes(files))


def _dependency_candidate_digest(lock: dict[str, Any]) -> str:
    return sha256_bytes(canonical_bytes({
        "formalDependencyManifestSha256": lock["formalDependencyManifestSha256"],
        "runtimeOverlaySha256": lock["runtimeOverlaySha256"],
        "candidateFilesSha256": lock["candidateFilesSha256"],
    }))


def _core_lock_entries(root: Path, spec: dict[str, Any]) -> list[dict[str, Any]]:
    result = []
    for core in spec["cores"]:
        identity = _lock_identity(worktree_identity(Path(core["root"])))
        descriptor = root / ".pfb/candidates/cores" / core["id"] / "retrom-core-candidate.json"
        identity.update({
            "id": core["id"],
            "candidateSha256": sha256_file(descriptor) if descriptor.exists() else None,
        })
        result.append(identity)
    return result


def _validate_lock_shape(lock: Any, data: Any, pfb_id: str) -> None:
    lock_fields = {
        "schemaVersion", "kind", "pfbId", "retrom", "runtime", "cores",
        "formalDependencyManifestSha256", "runtimeOverlaySha256", "candidateFilesSha256",
    }
    data_fields = {
        "schemaVersion", "kind", "pfbId", "candidateDigest",
        "migrationTreeSha256", "dataCompatibilityDigest",
    }
    if not isinstance(lock, dict) or set(lock) != lock_fields or lock.get("schemaVersion") != 1 or lock.get("kind") != "RETROM_PFB_LOCK_V1" or lock.get("pfbId") != pfb_id:
        raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", "lock")
    if not isinstance(data, dict) or set(data) != data_fields or data.get("schemaVersion") != 1 or data.get("kind") != "RETROM_PFB_DATA_LOCK_V1" or data.get("pfbId") != pfb_id:
        raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", "data-lock")
    for field in ("formalDependencyManifestSha256", "candidateFilesSha256"):
        if not lowercase_hex(lock[field], 64):
            raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", field)
    for field in ("candidateDigest", "migrationTreeSha256", "dataCompatibilityDigest"):
        if not lowercase_hex(data[field], 64):
            raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", field)
