#!/usr/bin/env python3
"""Controller for fast, isolated Personal Feature Branch dev containers."""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from scripts.local_user import LocalUserError, require_local_user

from .common import atomic_json, canonical_bytes, load_json
from .docker import (
    app_container_health, app_container_running, app_down, app_logs, app_restart, app_up,
    build_toolchain, ensure_workspace, gateway_down, gateway_preflight, gateway_up,
    import_provider_base, migrate_legacy_storage, set_selected, workspace_paths,
)
from .errors import PFBError
from .identity import app_origin, compose_project, pfb_id, runtime_origin_template
from .registry import locked_registry, register_spec, registry_entry, save_registry
from .source_tree import git_text, worktree_identity
from .spec import create_spec, load_spec, save_spec
from .state import load_state, write_state
from .toolchain import validate_host


class CommandFailure(RuntimeError):
    pass


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(prog="retrom-pfb")
    subparsers = result.add_subparsers(dest="command", required=True)
    commands = {
        "init", "validate", "build", "up", "use", "restart", "down", "status", "logs", "verify",
        "core-build", "provider-import", "migrate-storage", "data-reset", "remove", "destroy",
    }
    for command in sorted(commands):
        child = subparsers.add_parser(command)
        child.add_argument("--root", required=True, type=Path)
        child.add_argument("--pfb", required=True)
        if command == "init":
            child.add_argument("--runtime-root", type=Path)
            child.add_argument("--core-roots")
        if command == "up":
            child.add_argument("--select", choices=("true", "false"), default="true")
        if command == "status":
            child.add_argument("--format", choices=("text", "json"), default="text")
        if command == "logs":
            child.add_argument("--service", default="all")
        if command == "core-build":
            child.add_argument("--core", required=True)
        if command == "provider-import":
            child.add_argument("--source-root", required=True, type=Path)
        if command in {"provider-import", "migrate-storage", "data-reset", "remove", "destroy"}:
            child.add_argument("--confirm", required=True)
    for command in ("gateway-up", "gateway-down"):
        child = subparsers.add_parser(command)
        child.add_argument("--root", required=True, type=Path)
    return result


def main(arguments: list[str] | None = None) -> int:
    try:
        require_local_user()
        args = parser().parse_args(arguments)
        return dispatch(args)
    except LocalUserError as exc:
        print(str(exc), file=sys.stderr)
        return 2
    except PFBError as exc:
        print(str(exc), file=sys.stderr)
        return 2
    except CommandFailure as exc:
        print(str(exc), file=sys.stderr)
        return 1


def dispatch(args: argparse.Namespace) -> int:
    root = args.root.resolve(strict=True)
    return globals()[f"command_{args.command.replace('-', '_')}"](root, args)


def command_init(root: Path, args: argparse.Namespace) -> int:
    spec = create_spec(args.pfb, root, _absolute_optional(args.runtime_root), args.core_roots)
    existing = root / ".pfb/spec.json"
    if existing.exists() and canonical_bytes(load_json(existing)) != canonical_bytes(spec):
        raise PFBError("PFB_SPEC_INVALID", "already-initialized")
    save_spec(root, spec)
    write_state(root, spec["id"], "INITIALIZED")
    with locked_registry() as (registry, path):
        register_spec(registry, spec)
        save_registry(path, registry)
    _result({"id": spec["id"], "url": app_origin(spec["id"]), "status": "INITIALIZED"})
    return 0


def command_validate(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    toolchain = validate_host(root)
    gateway_preflight(root)
    _validate_branch_policy(spec)
    _result({"id": spec["id"], "status": load_state(root, spec["id"])["status"],
             "url": app_origin(spec["id"]), "runtimeOriginTemplate": runtime_origin_template(spec["id"]),
             "toolchain": toolchain, "workspace": str(workspace_paths(root)["root"])})
    return 0


def command_build(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    validate_host(root, require_browser=False)
    _validate_branch_policy(spec)
    if app_container_running(compose_project(spec["id"])):
        raise PFBError("PFB_SPEC_INVALID", "running-toolchain-build")
    try:
        result = build_toolchain(root, spec)
        write_state(root, spec["id"], "READY")
        _registry_status(spec["id"], "READY")
        _result({"id": spec["id"], "status": "READY", **result})
        return 0
    except (PFBError, CommandFailure) as exc:
        write_state(root, spec["id"], "ERROR", error=str(exc))
        _registry_status(spec["id"], "ERROR")
        raise


def command_up(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    validate_host(root, require_browser=False)
    gateway_up(root)
    app_up(root, spec)
    _wait_healthy(compose_project(spec["id"]))
    write_state(root, spec["id"], "RUNNING")
    _registry_status(spec["id"], "RUNNING")
    if args.select == "true":
        _select_running(root, spec["id"])
    _result({"id": spec["id"], "status": "RUNNING", "url": app_origin(spec["id"])})
    return 0


def command_use(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    _select_running(root, spec["id"])
    _result({"id": spec["id"], "selected": True, "redirect": app_origin(spec["id"])})
    return 0


def command_restart(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    validate_host(root, require_browser=False)
    if not app_container_running(compose_project(spec["id"])):
        raise PFBError("PFB_SELECTED_TARGET_UNAVAILABLE", "not-running")
    app_restart(root, spec)
    _wait_healthy(compose_project(spec["id"]))
    write_state(root, spec["id"], "RUNNING")
    _registry_status(spec["id"], "RUNNING")
    _result({"id": spec["id"], "status": "RUNNING", "url": app_origin(spec["id"])})
    return 0


def command_down(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    _down(root, spec)
    _result({"id": spec["id"], "status": "STOPPED", "workspacePreserved": True})
    return 0


def command_status(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    state = load_state(root, spec["id"])
    running = app_container_running(compose_project(spec["id"]))
    descriptor = workspace_paths(root)["providerDev"] / "dev-provider.json"
    development = load_json(descriptor) if descriptor.is_file() else None
    result = {
        "id": spec["id"], "name": spec["name"], "status": "RUNNING" if running else state["status"],
        "health": app_container_health(compose_project(spec["id"])), "url": app_origin(spec["id"]),
        "runtimeOriginTemplate": runtime_origin_template(spec["id"]),
        "workspace": str(workspace_paths(root)["root"]),
        "providerDevRevision": development.get("revision") if isinstance(development, dict) else None,
        "git": {"branch": git_text(root, ["symbolic-ref", "--short", "HEAD"], "PFB_WORKTREE_INVALID"),
                "commit": git_text(root, ["rev-parse", "HEAD"], "PFB_WORKTREE_INVALID"),
                "dirty": bool(git_text(root, ["status", "--porcelain=v1", "--untracked-files=all"],
                                       "PFB_WORKTREE_INVALID"))},
    }
    if args.format == "json":
        print(canonical_bytes(result).decode("utf-8"))
    else:
        _result(result)
    return 0


def command_logs(root: Path, args: argparse.Namespace) -> int:
    return app_logs(root, _named_spec(root, args.pfb), args.service)


def command_verify(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    if not app_container_running(compose_project(spec["id"])) or \
            app_container_health(compose_project(spec["id"])) != "healthy":
        raise CommandFailure("PFB_VERIFY_FAILED:upstream")
    completed = subprocess.run([sys.executable, "scripts/test_pfb.py"], cwd=root,
                               capture_output=True, text=True, check=False)
    run_id = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    evidence = root / ".pfb/evidence" / run_id
    evidence.mkdir(parents=True, mode=0o700)
    (evidence / "output.log").write_text(completed.stdout + completed.stderr, encoding="utf-8")
    descriptor = workspace_paths(root)["providerDev"] / "dev-provider.json"
    run = {"schemaVersion": 2, "pfbId": spec["id"], "origin": app_origin(spec["id"]),
           "runtimeOriginTemplate": runtime_origin_template(spec["id"]), "workspace": str(workspace_paths(root)["root"]),
           "providerDev": load_json(descriptor) if descriptor.is_file() else None,
           "checks": [{"id": "ACC-PFB-DEV-001", "status": "PASSED" if completed.returncode == 0 else "FAILED"}]}
    atomic_json(evidence / "run.json", run)
    if completed.returncode != 0:
        raise CommandFailure("PFB_VERIFY_FAILED:contracts")
    _result({"id": spec["id"], "status": "VERIFIED", "evidence": str(evidence)})
    return 0


def command_core_build(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    core = next((item for item in spec["cores"] if item["id"] == args.core), None)
    if core is None:
        raise PFBError("PFB_SPEC_INVALID", "unknown-core")
    wrapper = Path(core["root"]) / ".github/rpg-runtime/build-candidate.sh"
    if not wrapper.is_file() or not os.access(wrapper, os.X_OK):
        raise PFBError("PFB_CORE_CANDIDATE_INTERFACE_MISSING", args.core)
    build_root = ensure_workspace(root)["root"] / "core-builds" / args.core
    build_root.mkdir(parents=True, exist_ok=True, mode=0o700)
    with tempfile.TemporaryDirectory(prefix="build.", dir=build_root) as temporary:
        completed = subprocess.run([str(wrapper), temporary], cwd=core["root"], check=False)
        if completed.returncode != 0:
            raise CommandFailure("PFB_CORE_BUILD_FAILED")
        descriptor = Path(temporary) / "retrom-core-candidate.json"
        value = load_json(descriptor, "PFB_CORE_CANDIDATE_INTERFACE_MISSING")
        if not isinstance(value, dict) or value.get("coreId") != args.core:
            raise PFBError("PFB_CORE_CANDIDATE_INTERFACE_MISSING", args.core)
        target = build_root / "current"
        previous = build_root / ".previous"
        shutil.rmtree(previous, ignore_errors=True)
        if target.exists():
            target.rename(previous)
        Path(temporary).rename(target)
        shutil.rmtree(previous, ignore_errors=True)
    _result({"id": spec["id"], "core": args.core, "output": str(target)})
    return 0


def command_provider_import(root: Path, args: argparse.Namespace) -> int:
    from scripts.runtime_providers import (
        check_active_providers,
        check_active_providers_for_upgrade,
        verify_provider_upgrade,
    )

    spec = _confirmed_spec(root, args.pfb, args.confirm)
    if app_container_running(compose_project(spec["id"])):
        raise PFBError("PFB_PROVIDER_BASE_INVALID", "running")

    def validate(active_path: Path, installed_root: Path) -> dict[str, Any]:
        value = load_json(active_path, "PFB_PROVIDER_BASE_INVALID")
        source = value.get("source") if isinstance(value, dict) else None
        if source not in {"candidate", "production"}:
            raise PFBError("PFB_PROVIDER_BASE_INVALID", "source")
        return check_active_providers(active_path, installed_root, source)

    def validate_current(active_path: Path, installed_root: Path) -> dict[str, Any]:
        value = load_json(active_path, "PFB_PROVIDER_BASE_INVALID")
        source = value.get("source") if isinstance(value, dict) else None
        if source not in {"candidate", "production"}:
            raise PFBError("PFB_PROVIDER_BASE_INVALID", "source")
        return check_active_providers_for_upgrade(active_path, installed_root, source)

    result = import_provider_base(
        root,
        args.source_root,
        validate,
        lambda current, incoming: verify_provider_upgrade(current, incoming, []),
        validate_current=validate_current,
    )
    _result({"id": spec["id"], "status": "IMPORTED", **result})
    return 0


def command_migrate_storage(root: Path, args: argparse.Namespace) -> int:
    spec = _confirmed_spec(root, args.pfb, args.confirm)
    if app_container_running(compose_project(spec["id"])):
        raise PFBError("PFB_STORAGE_MIGRATION_INVALID", "running")
    raw_state = load_json(root / ".pfb/state.json") if (root / ".pfb/state.json").is_file() else {}
    preferred = raw_state.get("dataCompatibilityDigest") if isinstance(raw_state, dict) else None
    volumes = migrate_legacy_storage(root, spec, preferred if isinstance(preferred, str) else None)
    _result({"id": spec["id"], "workspace": str(workspace_paths(root)["root"]),
             "legacyVolumes": volumes, "legacyVolumesPreserved": True})
    return 0


def command_data_reset(root: Path, args: argparse.Namespace) -> int:
    spec = _confirmed_spec(root, args.pfb, args.confirm)
    if app_container_running(compose_project(spec["id"])):
        raise PFBError("PFB_DATA_RESET_INVALID", "running")
    paths = ensure_workspace(root)
    backup = paths["root"] / "reset-backups" / datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    backup.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    if paths["data"].exists():
        paths["data"].rename(backup)
    paths["data"].mkdir(mode=0o700)
    _result({"id": spec["id"], "status": "RESET", "backup": str(backup)})
    return 0


def command_remove(root: Path, args: argparse.Namespace) -> int:
    spec = _confirmed_spec(root, args.pfb, args.confirm)
    _remove_registration(root, spec)
    _result({"id": spec["id"], "status": "REMOVED", "workspacePreserved": True})
    return 0


def command_destroy(root: Path, args: argparse.Namespace) -> int:
    spec = _confirmed_spec(root, args.pfb, args.confirm)
    _remove_registration(root, spec)
    shutil.rmtree(root / ".pfb")
    _result({"id": spec["id"], "status": "DESTROYED", "legacyVolumesPreserved": True})
    return 0


def command_gateway_up(root: Path, _args: argparse.Namespace) -> int:
    validate_host(root, require_browser=False)
    _result({"status": "RUNNING", "gateway": gateway_up(root), "url": "http://localhost:3000"})
    return 0


def command_gateway_down(root: Path, _args: argparse.Namespace) -> int:
    gateway_down(root)
    _result({"status": "STOPPED"})
    return 0


def _down(root: Path, spec: dict[str, Any]) -> None:
    with locked_registry() as (registry, path):
        if registry["selectedPfbId"] == spec["id"]:
            registry["selectedPfbId"] = None
            save_registry(path, registry)
            set_selected(root, None)
    app_down(root, spec)
    write_state(root, spec["id"], "STOPPED")
    _registry_status(spec["id"], "STOPPED")


def _remove_registration(root: Path, spec: dict[str, Any]) -> None:
    app_down(root, spec)
    with locked_registry() as (registry, path):
        registry["pfbs"] = [item for item in registry["pfbs"] if item["id"] != spec["id"]]
        if registry["selectedPfbId"] == spec["id"]:
            registry["selectedPfbId"] = None
            set_selected(root, None)
        save_registry(path, registry)


def _confirmed_spec(root: Path, name: str, confirmation: str) -> dict[str, Any]:
    spec = _named_spec(root, name)
    if confirmation != spec["id"]:
        raise PFBError("PFB_SPEC_INVALID", "confirmation")
    return spec


def _validate_branch_policy(spec: dict[str, Any]) -> None:
    sources = [spec["retrom"], *([spec["runtime"]] if spec["runtime"]["mode"] == "branch" else [])]
    for source in sources:
        worktree_identity(Path(source["root"]))
    for core in spec["cores"]:
        root = Path(core["root"])
        identity = worktree_identity(root)
        fork = load_json(root / "retrom-fork.json", "PFB_BRANCH_POLICY_INVALID")
        if not isinstance(fork, dict) or fork.get("schemaVersion") != 1 or not isinstance(fork.get("defaultBranch"), str):
            raise PFBError("PFB_BRANCH_POLICY_INVALID", core["id"])
        if not any(identity["branch"].startswith(prefix) for prefix in ("fix/", "feat/", "build/", "sync/upstream-")):
            raise PFBError("PFB_BRANCH_POLICY_INVALID", core["id"])
        if subprocess.run(["git", "-C", str(root), "merge-base", "--is-ancestor", fork["defaultBranch"], "HEAD"],
                          check=False).returncode != 0:
            raise PFBError("PFB_BRANCH_POLICY_INVALID", core["id"])


def _select_running(root: Path, pfb_id_value: str) -> None:
    if not app_container_running(compose_project(pfb_id_value)):
        raise PFBError("PFB_SELECTED_TARGET_UNAVAILABLE")
    with locked_registry() as (registry, path):
        entry = registry_entry(registry, pfb_id_value)
        if entry["status"] != "RUNNING":
            raise PFBError("PFB_SELECTED_TARGET_UNAVAILABLE")
        registry["selectedPfbId"] = pfb_id_value
        save_registry(path, registry)
    set_selected(root, pfb_id_value)


def _registry_status(pfb_id_value: str, status: str) -> None:
    with locked_registry() as (registry, path):
        registry_entry(registry, pfb_id_value)["status"] = status
        save_registry(path, registry)


def _wait_healthy(project: str) -> None:
    deadline = time.monotonic() + 300
    while time.monotonic() < deadline:
        if app_container_health(project) == "healthy":
            return
        if not app_container_running(project):
            break
        time.sleep(1)
    raise CommandFailure("PFB_UPSTREAM_UNAVAILABLE")


def _named_spec(root: Path, name: str) -> dict[str, Any]:
    spec = load_spec(root)
    if spec["name"] != name or spec["id"] != pfb_id(name):
        raise PFBError("PFB_SPEC_INVALID", "name")
    with locked_registry() as (registry, _path):
        entry = registry_entry(registry, spec["id"])
        if entry["retromRoot"] != str(root):
            raise PFBError("PFB_WORKTREE_INVALID", "registry")
    return spec


def _absolute_optional(value: Path | None) -> Path | None:
    if value is None:
        return None
    if not value.is_absolute():
        raise PFBError("PFB_WORKTREE_INVALID")
    return value


def _result(value: dict[str, Any]) -> None:
    print(json.dumps(value, ensure_ascii=False, sort_keys=True))


if __name__ == "__main__":
    raise SystemExit(main())
