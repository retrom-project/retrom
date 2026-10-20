"""Run the single Go chunk synchronization implementation (cmd/retrom-providers) for tooling."""
from __future__ import annotations

import json
import os
import subprocess
from pathlib import Path
from typing import Any, Iterable

ROOT = Path(__file__).resolve().parents[1]


def retrom_binary(root: Path = ROOT) -> str:
    """Use RETROM_BINARY, or build this checkout's Provider command incrementally into .cache/bin.

    cmd/retrom-providers depends only on internal/providersync, so it builds on a clean checkout
    without generated API code; the image runs the same code as `retrom providers`.
    """
    configured = os.environ.get("RETROM_BINARY")
    if configured:
        return configured
    output = root / ".cache/bin/retrom-providers"
    output.parent.mkdir(parents=True, exist_ok=True)
    result = subprocess.run(["go", "build", "-o", str(output), "./cmd/retrom-providers"], cwd=root,
                            capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise ValueError("RUNTIME_PROVIDER_SYNC_BUILD_FAILED: " + result.stderr.strip()[-2000:])
    return str(output)


def sync_providers(inputs: Path, providers_root: Path, *, cache: Path | None = None, source: str = "",
                   seeds: Iterable[Path] = (), full: bool = False, mode: str = "tooling") -> dict[str, Any]:
    """Install the pinned Providers into providers_root (active.json + installed/) from chunks."""
    command = [retrom_binary(), "verify" if full else "sync", "--inputs", str(inputs),
               "--root", str(providers_root), "--mode", mode]
    if cache is not None:
        command += ["--cache", str(cache)]
    if source:
        command += ["--source", source]
    for seed in seeds:
        command += ["--seed", str(seed)]
    result = subprocess.run(command, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise ValueError("RUNTIME_PROVIDER_SYNC_FAILED: " + result.stderr.strip()[-2000:])
    return json.loads(result.stdout)
