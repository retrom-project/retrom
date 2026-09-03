#!/usr/bin/env python3
"""Compute the deterministic, production-only image input digest."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import stat
import subprocess
import sys
from pathlib import Path, PurePosixPath

from dependencies import CheckError, parse_versions
from runtime_provider_bundle import validate_provider_lock


ROOT = Path(__file__).resolve().parent.parent
PROVIDER_IDS = ("emulatorjs", "retrom-runtime")


def canonical(value: object) -> bytes:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode()


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def source_entries() -> list[dict[str, object]]:
    result = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=ROOT,
        check=True,
        stdout=subprocess.PIPE,
    )
    entries: list[dict[str, object]] = []
    for raw in result.stdout.split(b"\0"):
        if not raw:
            continue
        try:
            relative = raw.decode("utf-8")
        except UnicodeDecodeError as exc:
            raise ValueError("RELEASE_INPUT_PATH_NOT_UTF8") from exc
        path = PurePosixPath(relative)
        if path.is_absolute() or any(part in ("", ".", "..") for part in path.parts):
            raise ValueError("RELEASE_INPUT_PATH_INVALID")
        absolute = ROOT / relative
        try:
            info = absolute.lstat()
        except FileNotFoundError:
            continue
        if stat.S_ISLNK(info.st_mode):
            mode = "120000"
            content = os.readlink(absolute).encode()
        elif stat.S_ISREG(info.st_mode):
            mode = "100755" if info.st_mode & stat.S_IXUSR else "100644"
            content = absolute.read_bytes()
        else:
            raise ValueError("RELEASE_INPUT_FILE_TYPE_INVALID")
        entries.append({"path": relative, "mode": mode, "sha256": sha256(content)})
    entries.sort(key=lambda entry: str(entry["path"]).encode())
    return entries


def provider_lock_entries() -> list[dict[str, object]]:
    root = ROOT / "data/runtime-providers"
    if any((ROOT / relative).exists() for relative in (
        ".pfb/candidates/runtime/providers",
        "data/runtime-providers/active.json",
        "data/runtime-providers/candidate-active.json",
        "data/runtime-providers/installed",
        "data/runtime-providers/cache",
        "data/runtime-providers/archive",
    )):
        raise ValueError("RELEASE_INPUT_CANDIDATE_OR_MUTABLE_PROVIDER_FORBIDDEN")
    paths = [root / f"{provider_id}.lock.json" for provider_id in PROVIDER_IDS]
    if any(not path.is_file() or path.is_symlink() for path in paths):
        raise ValueError("RELEASE_INPUT_PROVIDER_LOCKS_MISSING")
    entries = []
    release_identity: tuple[str, str, str] | None = None
    for provider_id, path in zip(PROVIDER_IDS, paths, strict=True):
        raw = path.read_bytes()
        lock = validate_provider_lock(json.loads(raw))
        if lock["providerId"] != provider_id:
            raise ValueError("RELEASE_INPUT_PROVIDER_LOCK_INVALID")
        identity = (lock["repository"], lock["tag"], lock["commit"])
        if release_identity is None:
            release_identity = identity
        elif identity != release_identity:
            raise ValueError("RELEASE_INPUT_PROVIDER_RELEASE_MISMATCH")
        entries.append({
            "bundleSha256": lock["bundleSha256"],
            "lockSha256": sha256(raw),
            "providerId": provider_id,
            "providerVersion": lock["providerVersion"],
        })
    return entries


def release_input_value(versions: list[str], active: str) -> dict[str, object]:
    normalized = parse_versions(",".join(versions))
    if normalized != versions:
        raise ValueError("RELEASE_INPUT_DEPENDENCY_VERSIONS_INVALID")
    if active not in versions:
        raise ValueError("RELEASE_INPUT_ACTIVE_VERSION_INVALID")
    dependency_versions = []
    for version in versions:
        manifest_bytes = (ROOT / "data/dat/emulatorjs" / version / "manifest.json").read_bytes()
        manifest = json.loads(manifest_bytes)
        if manifest.get("schema_version") != 8 or "player_adapter" in manifest.get("emulatorjs", {}):
            raise ValueError("RELEASE_INPUT_DAT_MANIFEST_INVALID")
        dependency_versions.append({
            "manifestSha256": sha256(manifest_bytes),
            "version": version,
        })
    return {
        "schemaVersion": 2,
        "sourceTreeSha256": sha256(canonical(source_entries())),
        "dependencyVersions": dependency_versions,
        "activeEmulatorjsVersion": active,
        "passwordBlocklistManifestSha256": sha256(
            (ROOT / "data/auth/password-blocklists/v1/manifest.json").read_bytes()
        ),
        "netplayManifestSha256": sha256((ROOT / "data/netplay/v2/manifest.json").read_bytes()),
        "runtimeTargetCatalogSha256": sha256(
            (ROOT / "data/runtime-target-bindings/v1/catalog.json").read_bytes()
        ),
        "providerLocks": provider_lock_entries(),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--versions", required=True)
    parser.add_argument("--active", required=True)
    args = parser.parse_args()
    release_input = release_input_value(parse_versions(args.versions), args.active)
    print(sha256(canonical(release_input)))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, CheckError, KeyError, json.JSONDecodeError, subprocess.CalledProcessError) as exc:
        print(str(exc), file=sys.stderr)
        raise SystemExit(1) from exc
