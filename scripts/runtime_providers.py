#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import re
import shutil
from pathlib import Path
from typing import Any

if __package__:
    from scripts.runtime_provider_io import _load_json, _write_json_atomic
    from scripts.runtime_input_manifest import release_identity as _valid_release_identity
    from scripts.runtime_provider_bundle import describe_installed_provider
    from scripts.runtime_provider_sync import sync_providers
else:
    from runtime_provider_io import _load_json, _write_json_atomic
    from runtime_input_manifest import release_identity as _valid_release_identity
    from runtime_provider_bundle import describe_installed_provider
    from runtime_provider_sync import sync_providers


PROVIDER_ID = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$")
SEMVER = re.compile(
    r"^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"
    r"(?:-(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
    r"(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?$"
)
LOWER_DIGEST = re.compile(r"^[0-9a-f]{64}$")


def prepare_candidate_providers(candidate_root: Path, providers_root: Path) -> dict[str, Any]:
    """Install a local runtime candidate (its release/ directory of chunks) without the network.

    The candidate is assembled in a staging root seeded from the current one, checked against
    the upgrade rules, and only then swapped in, so a refused candidate changes nothing.
    """
    candidate_root = candidate_root.resolve(strict=True)
    providers_root = providers_root.resolve()
    current = None
    if (providers_root / "active.json").exists():
        current = _load_json(providers_root / "active.json", "RUNTIME_PROVIDER_ACTIVE_INVALID")
        if current.get("source") == "production":
            raise ValueError("RUNTIME_PROVIDER_PRODUCTION_FORBIDDEN")
    staging = providers_root.with_name(f".{providers_root.name}-candidate")
    previous = providers_root.with_name(f".{providers_root.name}-previous")
    shutil.rmtree(staging, ignore_errors=True)
    shutil.rmtree(previous, ignore_errors=True)
    try:
        sync_providers(candidate_root / "runtime-inputs.json", staging, source=str(candidate_root),
                       seeds=[providers_root] if providers_root.exists() else [])
        active = check_active_providers(staging / "active.json", staging / "installed", "candidate")
        if current is not None:
            verify_provider_upgrade(current, active, [])
        providers_root.parent.mkdir(parents=True, exist_ok=True)
        if providers_root.exists():
            providers_root.rename(previous)
        staging.rename(providers_root)
    finally:
        shutil.rmtree(staging, ignore_errors=True)
        shutil.rmtree(previous, ignore_errors=True)
    return active


def check_active_providers(active_path: Path, installed_root: Path, expected_source: str) -> dict[str, Any]:
    active = _load_json(active_path, "RUNTIME_PROVIDER_ACTIVE_INVALID")
    if set(active) != {"providers", "release", "schemaVersion", "source", "sourceTreeSha256"} or \
            active["schemaVersion"] != 1 or active["source"] != expected_source or \
            expected_source not in {"candidate", "production"}:
        raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
    if expected_source == "candidate":
        if active["release"] is not None or not _match(LOWER_DIGEST, active["sourceTreeSha256"]):
            raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
    elif not _valid_release_identity(active["release"]) or active["sourceTreeSha256"] is not None:
        raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
    providers = _active_providers(active)
    for provider in providers.values():
        if expected_source == "production" and provider["providerVersion"] != active["release"]["tag"][1:]:
            raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
        # An active entry carries its own bundle identity; rebuilding it from disk must match exactly.
        if describe_installed_provider(provider, installed_root) != provider:
            raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
    return active


def verify_provider_upgrade(
    current: dict[str, Any],
    candidate: dict[str, Any],
    checkpoint_references: list[dict[str, str]],
    *,
    allow_candidate_rebuild: bool = False,
) -> None:
    current_by_id = _active_providers(current)
    candidate_by_id = _active_providers(candidate)
    for provider_id, previous in current_by_id.items():
        proposed = candidate_by_id.get(provider_id)
        if proposed is None:
            raise ValueError("RUNTIME_PROVIDER_REFERENCED_TARGET_REMOVED")
        ordering = _compare_semver(proposed["providerVersion"], previous["providerVersion"])
        if ordering < 0:
            raise ValueError("RUNTIME_PROVIDER_DOWNGRADE_FORBIDDEN")
        explicit_candidate = allow_candidate_rebuild and candidate.get("source") == "candidate"
        if ordering == 0 and proposed["bundleSha256"] != previous["bundleSha256"] and not explicit_candidate:
            raise ValueError("RUNTIME_PROVIDER_VERSION_REBUILT")
    for reference in checkpoint_references:
        if not isinstance(reference, dict) or set(reference) != {"format", "providerId", "targetId"}:
            raise ValueError("RUNTIME_PROVIDER_CHECKPOINT_REFERENCE_INVALID")
        provider = candidate_by_id.get(reference["providerId"])
        target = next((item for item in provider["targets"] if item["id"] == reference["targetId"]), None) \
            if provider else None
        checkpoint = target.get("checkpoint") if target else None
        if not checkpoint or reference["format"] not in checkpoint["readFormats"]:
            raise ValueError("RUNTIME_PROVIDER_CHECKPOINT_FORMAT_UNREADABLE")


def _active_providers(value: dict[str, Any]) -> dict[str, dict[str, Any]]:
    providers = value.get("providers") if isinstance(value, dict) else None
    if not isinstance(providers, list) or not providers:
        raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
    result: dict[str, dict[str, Any]] = {}
    for provider in providers:
        if not isinstance(provider, dict) or not _match(PROVIDER_ID, provider.get("providerId")) or \
                not _match(SEMVER, provider.get("providerVersion")) or \
                not _match(LOWER_DIGEST, provider.get("bundleSha256")) or not isinstance(provider.get("targets"), list):
            raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
        if provider["providerId"] in result:
            raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
        result[provider["providerId"]] = provider
    return result


def _compare_semver(left: str, right: str) -> int:
    def parse(value: str) -> tuple[tuple[int, int, int], list[str] | None]:
        match = SEMVER.fullmatch(value)
        if not match:
            raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
        main, separator, prerelease = value.partition("-")
        return tuple(int(part) for part in main.split(".")), prerelease.split(".") if separator else None

    left_main, left_pre = parse(left)
    right_main, right_pre = parse(right)
    if left_main != right_main:
        return 1 if left_main > right_main else -1
    if left_pre is None or right_pre is None:
        return 0 if left_pre is right_pre else 1 if left_pre is None else -1
    for left_part, right_part in zip(left_pre, right_pre):
        if left_part == right_part:
            continue
        if left_part.isdigit() and right_part.isdigit():
            return 1 if int(left_part) > int(right_part) else -1
        if left_part.isdigit() != right_part.isdigit():
            return -1 if left_part.isdigit() else 1
        return 1 if left_part > right_part else -1
    return (len(left_pre) > len(right_pre)) - (len(left_pre) < len(right_pre))


def _match(pattern: re.Pattern[str], value: Any) -> bool:
    return isinstance(value, str) and pattern.fullmatch(value) is not None








def main() -> int:
    parser = argparse.ArgumentParser(description="Manage immutable Retrom runtime Provider bundles")
    subcommands = parser.add_subparsers(dest="command", required=True)
    prepare = subcommands.add_parser("prepare-candidate")
    prepare.add_argument("--candidate-root", type=Path, required=True)
    prepare.add_argument("--providers-root", type=Path, required=True)
    check = subcommands.add_parser("check")
    check.add_argument("--active-path", type=Path, required=True)
    check.add_argument("--installed-root", type=Path, required=True)
    check.add_argument("--source", choices=("candidate", "production"), required=True)
    upgrade = subcommands.add_parser("verify-upgrade")
    upgrade.add_argument("--current", type=Path, required=True)
    upgrade.add_argument("--candidate", type=Path, required=True)
    upgrade.add_argument("--checkpoint-references", type=Path, required=True)
    arguments = parser.parse_args()
    if arguments.command == "prepare-candidate":
        result = prepare_candidate_providers(arguments.candidate_root, arguments.providers_root)
    elif arguments.command == "check":
        result = check_active_providers(arguments.active_path, arguments.installed_root, arguments.source)
    elif arguments.command == "verify-upgrade":
        current = _load_json(arguments.current, "RUNTIME_PROVIDER_ACTIVE_INVALID")
        candidate = _load_json(arguments.candidate, "RUNTIME_PROVIDER_ACTIVE_INVALID")
        references = _load_json(arguments.checkpoint_references, "RUNTIME_PROVIDER_CHECKPOINT_REFERENCE_INVALID")
        if not isinstance(references, list):
            raise ValueError("RUNTIME_PROVIDER_CHECKPOINT_REFERENCE_INVALID")
        verify_provider_upgrade(current, candidate, references)
        result = {"status": "ok"}
    else:
        raise AssertionError(arguments.command)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
