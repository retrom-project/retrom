#!/usr/bin/env python3
"""Check immutable published SQL files, or generate a release migration manifest."""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
NAME = re.compile(r"migrations/([0-9]{3})_[a-z0-9_]+\.sql\Z")


def git(root: Path, *arguments: str) -> bytes:
    return subprocess.check_output(["git", "-C", str(root), *arguments], stderr=subprocess.PIPE)


def validate_names(names: list[str]) -> None:
    if not names:
        raise ValueError("migration list is empty")
    for number, name in enumerate(sorted(names), 1):
        match = NAME.fullmatch(name)
        if match is None or match[1] != f"{number:03d}":
            raise ValueError(f"migration must have a unique consecutive number: {name}")


def snapshot(root: Path, reference: str, tag: str) -> dict:
    commit = git(root, "rev-parse", "--verify", f"{reference}^{{commit}}").decode().strip()
    names = [name.decode() for name in git(root, "ls-tree", "-r", "--name-only", "-z", commit,
                                         "--", "migrations").split(b"\0") if name.endswith(b".sql")]
    validate_names(names)
    return {
        "schemaVersion": 1, "releaseTag": tag, "releaseCommit": commit,
        "migrations": [{"path": name, "sha256": hashlib.sha256(git(root, "show", f"{commit}:{name}")).hexdigest()}
                       for name in sorted(names)],
    }


def latest_release(root: Path) -> dict:
    release = json.loads(subprocess.check_output([
        "gh", "api", "repos/retrom-project/retrom/releases/latest",
    ]))
    tag = release["tag_name"]
    if (release.get("draft") or release.get("prerelease") or not release.get("published_at")
            or not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", tag)):
        raise ValueError("latest release must be a published stable version")
    reference = "refs/ci/released-migrations"
    git(root, "fetch", "--no-tags", "origin", f"+refs/tags/{tag}:{reference}")
    return snapshot(root, reference, tag)


def check(root: Path, manifest: dict) -> None:
    if (set(manifest) != {"schemaVersion", "releaseTag", "releaseCommit", "migrations"}
            or manifest["schemaVersion"] != 1
            or not re.fullmatch(r"[0-9a-f]{40}", manifest["releaseCommit"])
            or not isinstance(manifest["releaseTag"], str)
            or not isinstance(manifest["migrations"], list)):
        raise ValueError("invalid published migration manifest")
    recorded = manifest["migrations"]
    for item in recorded:
        if (not isinstance(item, dict) or set(item) != {"path", "sha256"}
                or not isinstance(item["path"], str) or not NAME.fullmatch(item["path"])
                or not isinstance(item["sha256"], str) or not re.fullmatch(r"[0-9a-f]{64}", item["sha256"])):
            raise ValueError("invalid published migration entry")
    validate_names([item["path"] for item in recorded])
    names = [path.relative_to(root).as_posix() for path in (root / "migrations").rglob("*.sql")]
    validate_names(names)
    for item in recorded:
        path = root / item["path"]
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != item["sha256"]:
            raise ValueError(f"published migration changed or removed: {item['path']} ({manifest['releaseTag']})")
    print(f"migration-check: PASS ({len(recorded)} frozen by {manifest['releaseTag']}; {len(names)} current)")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("check", "snapshot"))
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--manifest", type=Path)
    parser.add_argument("--latest-release", action="store_true")
    parser.add_argument("--ref", default="HEAD")
    parser.add_argument("--release-tag")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    try:
        root = args.root.resolve()
        if args.action == "snapshot":
            if not args.release_tag or not args.output:
                raise ValueError("snapshot requires --release-tag and --output")
            result = snapshot(root, args.ref, args.release_tag)
            args.output.parent.mkdir(parents=True, exist_ok=True)
            args.output.write_text(json.dumps(result, indent=2) + "\n")
        else:
            manifest = (latest_release(root) if args.latest_release else
                        json.loads((args.manifest or root / "quality/released-migrations.json").read_text()))
            check(root, manifest)
    except (OSError, ValueError, TypeError, KeyError, subprocess.CalledProcessError) as error:
        print(f"migration-check: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
