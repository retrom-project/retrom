#!/usr/bin/env python3
"""Compute the deterministic release input digest shared by both images."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import stat
import subprocess
import sys
from pathlib import Path, PurePosixPath


ROOT = Path(__file__).resolve().parent.parent


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
        info = absolute.lstat()
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


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--versions", required=True)
    parser.add_argument("--active", required=True)
    args = parser.parse_args()
    versions = args.versions.split(",")
    if args.active not in versions:
        raise ValueError("RELEASE_INPUT_ACTIVE_VERSION_INVALID")
    dependency_versions: list[dict[str, str]] = []
    for version in versions:
        manifest_path = ROOT / "data/dat/emulatorjs" / version / "manifest.json"
        manifest_bytes = manifest_path.read_bytes()
        manifest = json.loads(manifest_bytes)
        dependency_versions.append(
            {
                "version": version,
                "manifestSha256": sha256(manifest_bytes),
                "playerAdapterId": manifest["emulatorjs"]["player_adapter"]["id"],
            }
        )
    release_input = {
        "schemaVersion": 1,
        "sourceTreeSha256": sha256(canonical(source_entries())),
        "dependencyVersions": dependency_versions,
        "activeEmulatorjsVersion": args.active,
    }
    print(sha256(canonical(release_input)))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, KeyError, json.JSONDecodeError, subprocess.CalledProcessError) as exc:
        print(str(exc), file=sys.stderr)
        raise SystemExit(1) from exc
