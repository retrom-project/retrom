#!/usr/bin/env python3
"""Build all project-owned deterministic arcade smoke fixtures."""

from __future__ import annotations

import argparse
import json
import shutil
import tempfile
from pathlib import Path
from typing import Any

import cps1_fixture
import cps2_fixture
import pacman_fixture


OUTPUT_ROOT = Path(__file__).resolve().parent
LAYOUT_PATH = OUTPUT_ROOT / "driver-layouts.json"


def load_drivers() -> dict[str, dict[str, Any]]:
    document = json.loads(LAYOUT_PATH.read_text(encoding="utf-8"))
    if document.get("schemaVersion") != 1 or set(document.get("drivers", {})) != {
        "1941",
        "spf2xjd",
    }:
        raise ValueError("unexpected CPS driver layout baseline")
    return document["drivers"]


def build_outputs() -> dict[str, bytes]:
    drivers = load_drivers()
    outputs = pacman_fixture.build_outputs()
    outputs.update(cps1_fixture.build_outputs(drivers["1941"]))
    outputs.update(cps2_fixture.build_outputs(drivers["spf2xjd"]))
    if len(outputs) != len(set(outputs)):
        raise ValueError("duplicate public arcade fixture output")
    return outputs


def check_outputs(outputs: dict[str, bytes]) -> None:
    for name, content in outputs.items():
        path = OUTPUT_ROOT / name
        if not path.is_file() or path.read_bytes() != content:
            raise SystemExit(f"public arcade fixture drifted: {name}; run build.py")


def write_outputs(outputs: dict[str, bytes]) -> None:
    with tempfile.TemporaryDirectory(prefix="retrom-arcade-smoke-", dir=OUTPUT_ROOT) as temporary:
        temporary_root = Path(temporary)
        for name, content in outputs.items():
            path = temporary_root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
            path.chmod(0o644)
        for name in outputs:
            destination = OUTPUT_ROOT / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.move(temporary_root / name, destination)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    arguments = parser.parse_args()
    outputs = build_outputs()
    if arguments.check:
        check_outputs(outputs)
    else:
        write_outputs(outputs)


if __name__ == "__main__":
    main()
