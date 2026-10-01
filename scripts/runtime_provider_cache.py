"""Locate and fill the immutable public Provider download cache."""
from __future__ import annotations

import argparse
import fcntl
import subprocess
from pathlib import Path
from typing import Any, Callable

if __package__:
    from scripts.runtime_provider_io import _write_bytes_atomic
else:
    from runtime_provider_io import _write_bytes_atomic


def default_cache_root(repository_root: Path) -> Path:
    repository_root = repository_root.resolve()
    local_cache = repository_root / ".cache/runtime-providers"
    try:
        result = subprocess.run(
            ["git", "-C", str(repository_root), "rev-parse", "--path-format=absolute", "--git-common-dir"],
            capture_output=True, text=True, check=False,
        )
    except FileNotFoundError:
        return local_cache
    if result.returncode != 0 or not result.stdout.strip():
        return local_cache
    common = Path(result.stdout.strip()).resolve()
    owner = common.parent
    workspace = owner.parent.parent
    if common.name == ".git" and owner.parent.name == "project" and (workspace / "manifest.yaml").is_file():
        return workspace / ".cache/runtime-providers"
    return local_cache


def cached_download(
    path: Path,
    download: Callable[[], bytes],
    validate: Callable[[bytes], Any],
    maximum_bytes: int,
) -> Any:
    """Serialize cache fills, validate every hit, and publish only complete bytes."""
    path.parent.mkdir(parents=True, exist_ok=True)
    # Keep the lock inode: unlinking it lets later callers bypass existing waiters.
    with path.with_name(f".{path.name}.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        try:
            cached = path.exists()
            if cached:
                with path.open("rb") as source:
                    contents = source.read(maximum_bytes + 1)
            else:
                contents = download()
            result = validate(contents)
            if not cached:
                _write_bytes_atomic(path, contents)
            return result
        finally:
            fcntl.flock(lock, fcntl.LOCK_UN)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository-root", type=Path, required=True)
    print(default_cache_root(parser.parse_args().repository_root))
