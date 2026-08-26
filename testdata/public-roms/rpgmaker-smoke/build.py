#!/usr/bin/env python3
"""Build Retrom's project-owned deterministic RPG Maker fixtures."""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parent
REPOSITORY_ROOT = ROOT.parents[2]
OUTPUT_DIRECTORIES = (
    "rpg2000", "rpg2000-compat", "rpg2003", "rpgxp", "rpgvx", "rpgvxace", "rpgmv",
    "malicious-rpgmv", "malicious-rpgmz", "negative-matrix",
)
OUTPUT_FILES = ("fixture-manifest.json",)


def generated_paths(root: Path) -> set[str]:
    paths: set[str] = set()
    for directory in OUTPUT_DIRECTORIES:
        target = root / directory
        if not target.is_dir():
            raise SystemExit(f"RPG Maker public fixture directory missing: {directory}")
        for candidate in target.rglob("*"):
            if candidate.is_symlink():
                raise SystemExit(f"RPG Maker public fixture must not contain symlinks: {candidate}")
            if candidate.is_file():
                paths.add(candidate.relative_to(root).as_posix())
    for name in OUTPUT_FILES:
        candidate = root / name
        if not candidate.is_file() or candidate.is_symlink():
            raise SystemExit(f"RPG Maker public fixture output missing: {name}")
        paths.add(name)
    return paths


def generate(destination: Path) -> None:
    environment = os.environ.copy()
    environment.setdefault("GOCACHE", str(REPOSITORY_ROOT / ".cache" / "go-build"))
    subprocess.run(
        [
            "go",
            "run",
            "./testdata/public-roms/rpgmaker-smoke/generator",
            "--source",
            str(ROOT),
            "--output",
            str(destination),
        ],
        cwd=REPOSITORY_ROOT,
        env=environment,
        check=True,
    )


def verify_manifest(root: Path, paths: set[str]) -> None:
    manifest = json.loads((root / "fixture-manifest.json").read_bytes())
    if manifest.get("schemaVersion") != 1 or manifest.get("generator") != "generator/*.go":
        raise SystemExit("RPG Maker public fixture manifest header is invalid")
    entries = manifest.get("files")
    if not isinstance(entries, list):
        raise SystemExit("RPG Maker public fixture manifest files are invalid")
    declared = {entry.get("path") for entry in entries if isinstance(entry, dict)}
    if declared != paths - {"fixture-manifest.json"} or len(declared) != len(entries):
        raise SystemExit("RPG Maker public fixture manifest path set drifted")


def compare(generated: Path) -> None:
    expected_paths = generated_paths(generated)
    actual_paths = generated_paths(ROOT)
    if actual_paths != expected_paths:
        missing = sorted(expected_paths - actual_paths)
        extra = sorted(actual_paths - expected_paths)
        raise SystemExit(f"RPG Maker public fixture path drift: missing={missing} extra={extra}")
    verify_manifest(generated, expected_paths)
    verify_manifest(ROOT, actual_paths)
    for relative in sorted(expected_paths):
        if (generated / relative).read_bytes() != (ROOT / relative).read_bytes():
            raise SystemExit(f"RPG Maker public fixture drifted: {relative}; run build.py")


def publish(generated: Path) -> None:
    expected_paths = generated_paths(generated)
    for directory in OUTPUT_DIRECTORIES:
        target = ROOT / directory
        if target.exists():
            shutil.rmtree(target)
        shutil.copytree(generated / directory, target)
    for name in OUTPUT_FILES:
        shutil.copyfile(generated / name, ROOT / name)
        (ROOT / name).chmod(0o644)
    if generated_paths(ROOT) != expected_paths:
        raise SystemExit("published RPG Maker public fixture path set drifted")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    arguments = parser.parse_args()
    temporary_parent = REPOSITORY_ROOT / ".cache" / "tmp"
    temporary_parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="rpgmaker-smoke-", dir=temporary_parent) as name:
        generated = Path(name)
        generate(generated)
        if arguments.check:
            compare(generated)
        else:
            publish(generated)


if __name__ == "__main__":
    main()
