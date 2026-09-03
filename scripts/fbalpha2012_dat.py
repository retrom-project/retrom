#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import inspect
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import tarfile
import tempfile
from typing import Any


REPOSITORY_ROOT = Path(__file__).resolve().parent.parent
ENUMERATOR_SOURCE = REPOSITORY_ROOT / "scripts/fbalpha2012-dat-enumerator.cpp"
SUPPORTED_CORES = {"fbalpha2012_cps1", "fbalpha2012_cps2"}
STATS_PREFIX = "RETROM_FBA2012_DAT_STATS="
MAX_ARCHIVE_MEMBERS = 20_000
MAX_ARCHIVE_BYTES = 256 << 20


class GenerationError(RuntimeError):
    pass


def digest(path: Path) -> tuple[int, str]:
    hasher = hashlib.sha256()
    size = 0
    with path.open("rb") as source:
        while chunk := source.read(1 << 20):
            hasher.update(chunk)
            size += len(chunk)
    return size, hasher.hexdigest()


def verify_archive(path: Path, config: dict[str, Any]) -> None:
    size, sha256 = digest(path)
    if size != config["archive_size_bytes"] or sha256 != config["archive_sha256"]:
        raise GenerationError("FBA2012_DAT_SOURCE_ARCHIVE_MISMATCH")


def safe_extract(archive_path: Path, destination: Path, expected_root: str) -> Path:
    total_size = 0
    with tarfile.open(archive_path, mode="r:gz") as archive:
        members = archive.getmembers()
        if len(members) > MAX_ARCHIVE_MEMBERS:
            raise GenerationError("FBA2012_DAT_SOURCE_ARCHIVE_LIMIT")
        for member in members:
            member_path = PurePosixPath(member.name)
            if (
                member_path.is_absolute()
                or not member_path.parts
                or member_path.parts[0] != expected_root
                or any(part in ("", ".", "..") for part in member_path.parts)
                or not (member.isdir() or member.isfile())
            ):
                raise GenerationError("FBA2012_DAT_SOURCE_ARCHIVE_UNSAFE")
            total_size += member.size
            if total_size > MAX_ARCHIVE_BYTES:
                raise GenerationError("FBA2012_DAT_SOURCE_ARCHIVE_LIMIT")
        extraction_options = {"filter": "data"} if "filter" in inspect.signature(archive.extractall).parameters else {}
        archive.extractall(destination, members=members, **extraction_options)
    source_root = destination / expected_root
    if not source_root.is_dir():
        raise GenerationError("FBA2012_DAT_SOURCE_ARCHIVE_INVALID")
    return source_root


def build_enumerator(source_root: Path, core_id: str, work_dir: Path) -> Path:
    make = shutil.which("make")
    compiler = shutil.which("g++")
    if make is None or compiler is None:
        raise GenerationError("FBA2012_DAT_NATIVE_TOOLCHAIN_MISSING")
    build = subprocess.run(
        [make, "-j2", "STATIC_LINKING=1", "platform=unix"],
        cwd=source_root,
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )
    if build.returncode != 0:
        raise GenerationError(f"FBA2012_DAT_NATIVE_BUILD_FAILED:{build.stdout[-4000:]}")
    archive = source_root / f"{core_id}_libretro.so"
    if not archive.is_file():
        raise GenerationError("FBA2012_DAT_NATIVE_ARCHIVE_MISSING")
    executable = work_dir / "fbalpha2012-dat-enumerator"
    compile_result = subprocess.run(
        [
            compiler,
            "-std=c++20",
            "-O2",
            str(ENUMERATOR_SOURCE),
            "-Wl,--whole-archive",
            str(archive),
            "-Wl,--no-whole-archive",
            "-ldl",
            "-pthread",
            "-o",
            str(executable),
        ],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )
    if compile_result.returncode != 0:
        raise GenerationError(f"FBA2012_DAT_ENUMERATOR_BUILD_FAILED:{compile_result.stdout[-4000:]}")
    return executable


def parse_stats(stderr: str, config: dict[str, Any]) -> dict[str, Any]:
    report_lines = [line for line in stderr.splitlines() if line.startswith(STATS_PREFIX)]
    if len(report_lines) != 1:
        raise GenerationError("FBA2012_DAT_STATS_MISSING")
    try:
        report = json.loads(report_lines[0][len(STATS_PREFIX) :])
    except json.JSONDecodeError as error:
        raise GenerationError("FBA2012_DAT_STATS_INVALID") from error
    if (
        report.get("machineCount") != config["expected_machine_count"]
        or report.get("normalizedExternalParents")
        != config["expected_normalized_external_parents"]
        or report.get("explicitBiosMachineCount") != 0
        or report.get("baseDependencyTargetCount") != 0
    ):
        raise GenerationError("FBA2012_DAT_STATS_MISMATCH")
    return report


def generate_once(
    archive_path: Path,
    core_id: str,
    config: dict[str, Any],
    work_dir: Path,
) -> tuple[bytes, dict[str, Any]]:
    extracted = work_dir / "source"
    extracted.mkdir()
    source_root = safe_extract(archive_path, extracted, str(config["archive_root"]))
    executable = build_enumerator(source_root, core_id, work_dir)
    generated = subprocess.run(
        [executable, core_id],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    stderr = generated.stderr.decode("utf-8", errors="replace")
    if generated.returncode != 0:
        raise GenerationError(f"FBA2012_DAT_ENUMERATION_FAILED:{stderr[-4000:]}")
    report = parse_stats(stderr, config)
    try:
        generated.stdout.decode("utf-8")
    except UnicodeDecodeError as error:
        raise GenerationError("FBA2012_DAT_OUTPUT_NOT_UTF8") from error
    return generated.stdout, report


def materialize(
    archive_path: Path,
    core_id: str,
    output_path: Path,
    config: dict[str, Any],
) -> dict[str, Any]:
    if core_id not in SUPPORTED_CORES:
        raise GenerationError("FBA2012_DAT_UNSUPPORTED_CORE")
    verify_archive(archive_path, config)
    with tempfile.TemporaryDirectory(prefix=f"retrom-{core_id}-first-") as first_dir:
        first_bytes, report = generate_once(archive_path, core_id, config, Path(first_dir))
    with tempfile.TemporaryDirectory(prefix=f"retrom-{core_id}-second-") as second_dir:
        second_bytes, second_report = generate_once(archive_path, core_id, config, Path(second_dir))
    if first_bytes != second_bytes or report != second_report:
        raise GenerationError("FBA2012_DAT_NONDETERMINISTIC")
    output_path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    descriptor, candidate_name = tempfile.mkstemp(prefix=f".{output_path.name}.", dir=output_path.parent)
    candidate = Path(candidate_name)
    try:
        with os.fdopen(descriptor, "wb") as output:
            output.write(first_bytes)
            output.flush()
            os.fsync(output.fileno())
        os.chmod(candidate, 0o600)
        os.replace(candidate, output_path)
    finally:
        candidate.unlink(missing_ok=True)
    return {
        **report,
        "sizeBytes": len(first_bytes),
        "sha256": hashlib.sha256(first_bytes).hexdigest(),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--core-id", choices=sorted(SUPPORTED_CORES), required=True)
    parser.add_argument("--source-archive", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, required=True)
    args = parser.parse_args()
    try:
        manifest = json.loads(args.manifest.read_text(encoding="utf-8"))
        core = next(entry for entry in manifest["cores"] if entry["core_id"] == args.core_id)
        source = core["dat"]["materialization"]
        config = {
            "archive_root": source["source_archive_root"],
            "archive_size_bytes": source["source_archive_size_bytes"],
            "archive_sha256": source["source_archive_sha256"],
            "expected_machine_count": core["parse_stats"]["machine_count"],
            "expected_normalized_external_parents": source[
                "expected_normalized_external_parents"
            ],
        }
        report = materialize(args.source_archive, args.core_id, args.output, config)
    except (GenerationError, OSError, KeyError, StopIteration, tarfile.TarError) as error:
        parser.exit(1, f"{error}\n")
    print(json.dumps(report, sort_keys=True, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
