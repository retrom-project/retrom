"""Create and verify per-run copies of the locked, owned RGSS fixtures."""
from __future__ import annotations
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import uuid

MARKER = "RetromAcceptanceRun.txt"
GENERATIONS = {"rpgxp", "rpgvx", "rpgvxace"}


def inventory(root: Path) -> dict:
    if not root.is_dir() or root.is_symlink():
        raise ValueError("RPG_RUN_ROOT_INVALID")
    files = {}
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise ValueError("RPG_RUN_SYMLINK")
        if path.is_file():
            with path.open("rb") as stream:
                sha = hashlib.file_digest(stream, "sha256").hexdigest()
            files[path.relative_to(root).as_posix()] = {"sizeBytes": path.stat().st_size, "sha256": sha}
    if not files or len(files) > 10000:
        raise ValueError("RPG_RUN_FILES_INVALID")
    return files


def marker_bytes(run_id: str) -> bytes:
    if str(uuid.UUID(run_id)) != run_id:
        raise ValueError("RPG_RUN_ID_INVALID")
    return ("RETROM_ACCEPTANCE_RUN:" + run_id + "\n").encode("ascii")


def receipt(original: Path, run_id: str) -> dict:
    data = marker_bytes(run_id)
    base = inventory(original)
    if MARKER in base or original.name not in GENERATIONS:
        raise ValueError("RPG_RUN_BASE_INVALID")
    return {"recipe": "OWNED_RGSS_COPY_WITH_RUN_MARKER_V1", "runId": run_id,
            "baseFilesSha256": hashlib.sha256(json.dumps(base, sort_keys=True).encode()).hexdigest(),
            "addedFile": {"logicalName": MARKER, "sizeBytes": len(data), "sha256": hashlib.sha256(data).hexdigest()}}


def validate(original: Path, derived: Path) -> dict:
    base, actual = inventory(original), inventory(derived)
    marker = derived / MARKER
    if set(actual) != {*base, MARKER} or any(actual[name] != value for name, value in base.items()):
        raise ValueError("RPG_RUN_SOURCE_CHANGED")
    if marker.stat().st_size > 100:
        raise ValueError("RPG_RUN_MARKER_INVALID")
    data = marker.read_bytes()
    run_id = data.decode("ascii").removeprefix("RETROM_ACCEPTANCE_RUN:").rstrip("\n")
    if data != marker_bytes(run_id):
        raise ValueError("RPG_RUN_MARKER_INVALID")
    return receipt(original, run_id)


def create(original: Path, derived: Path, run_id: str) -> dict:
    expected = receipt(original, run_id)
    shutil.copytree(original, derived)
    (derived / MARKER).write_bytes(marker_bytes(run_id))
    if validate(original, derived) != expected:
        raise ValueError("RPG_RUN_COPY_INVALID")
    return expected


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("create", "validate"))
    parser.add_argument("original", type=Path)
    parser.add_argument("derived", type=Path)
    args = parser.parse_args()
    value = create(args.original, args.derived, str(uuid.uuid4())) if args.operation == "create" else validate(args.original, args.derived)
    print(json.dumps(value))


if __name__ == "__main__":
    main()
