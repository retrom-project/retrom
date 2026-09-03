#!/usr/bin/env python3
"""Command-line controller for Personal Feature Branch environments."""

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

from .common import canonical_bytes, load_json, lowercase_hex, sha256_bytes, sha256_file
from .docker import (
    app_down,
    app_container_health,
    app_container_running,
    app_logs,
    app_up,
    gateway_down,
    gateway_preflight,
    gateway_up,
    remove_pfb_volumes,
    run_runtime_candidate_builder,
    set_selected,
)
from .errors import PFBError
from .identity import app_origin, compose_project, pfb_id, runtime_origin_template
from .locks import build_lock, current_locks, entrypoint_locks, publish_locks
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
    root_commands = {
        "init", "validate", "build", "up", "use", "restart", "down",
        "status", "logs", "verify", "prune", "destroy", "entrypoint-check",
    }
    for command in sorted(root_commands):
        child = subparsers.add_parser(command)
        child.add_argument("--root", required=True, type=Path)
        if command == "entrypoint-check":
            child.add_argument("--pfb-id", required=True)
        else:
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
        if command == "prune":
            child.add_argument("--keep", required=True, type=int)
            child.add_argument("--confirm", required=True)
        if command == "destroy":
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
    command = args.command.replace("-", "_")
    handler = globals()[f"command_{command}"]
    return handler(root, args)


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
    for core in spec["cores"]:
        wrapper = Path(core["root"]) / ".github/rpg-runtime/build-candidate.sh"
        if not wrapper.is_file() or not os.access(wrapper, os.X_OK):
            raise PFBError("PFB_CORE_CANDIDATE_INTERFACE_MISSING", core["id"])
    state = load_state(root, spec["id"])
    if state["status"] in {"INITIALIZED", "VALIDATED"}:
        state = write_state(root, spec["id"], "VALIDATED")
    _result({
        "id": spec["id"], "status": state["status"], "url": app_origin(spec["id"]),
        "runtimeOriginTemplate": runtime_origin_template(spec["id"]), "toolchain": toolchain,
    })
    return 0


def command_build(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    validate_host(root, require_browser=False)
    if app_container_running(compose_project(spec["id"])):
        raise PFBError("PFB_SPEC_INVALID", "running-build")
    _validate_branch_policy(spec)
    pfb_root = root / ".pfb"
    pfb_root.mkdir(mode=0o700, exist_ok=True)
    try:
        with tempfile.TemporaryDirectory(prefix="candidates.", dir=pfb_root) as temporary:
            staging = Path(temporary)
            _build_cores(spec, staging / "cores")
            _build_runtime(spec, root, staging / "runtime")
            _publish_directory(staging, root / ".pfb/candidates")
        lock, data_lock = build_lock(root, spec)
        _require_candidate_outputs(lock, spec)
        publish_locks(root, lock, data_lock)
        digest = sha256_bytes(canonical_bytes(lock))
        write_state(
            root, spec["id"], "BUILT", candidate_digest=digest,
            data_digest=data_lock["dataCompatibilityDigest"],
        )
        _registry_status(spec["id"], "BUILT")
        _result({
            "id": spec["id"], "status": "BUILT", "candidateDigest": digest,
            "dataCompatibilityDigest": data_lock["dataCompatibilityDigest"],
        })
        return 0
    except (PFBError, CommandFailure) as exc:
        write_state(root, spec["id"], "BUILD_FAILED", error=str(exc))
        _registry_status(spec["id"], "BUILD_FAILED")
        raise


def command_up(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    validate_host(root, require_browser=False)
    _, _, candidate_digest, data_digest = _checked_current_locks(root, spec)
    gateway_up(root)
    app_up(root, spec, data_digest)
    _wait_healthy(compose_project(spec["id"]))
    write_state(root, spec["id"], "RUNNING", candidate_digest=candidate_digest, data_digest=data_digest)
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
    _, _, candidate_digest, data_digest = _checked_current_locks(root, spec)
    app_up(root, spec, data_digest, restart=True)
    _wait_healthy(compose_project(spec["id"]))
    write_state(root, spec["id"], "RUNNING", candidate_digest=candidate_digest, data_digest=data_digest)
    _registry_status(spec["id"], "RUNNING")
    _result({"id": spec["id"], "status": "RUNNING", "url": app_origin(spec["id"])})
    return 0


def command_down(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    _down(root, spec)
    _result({"id": spec["id"], "status": "STOPPED"})
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


def command_status(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    state = load_state(root, spec["id"])
    stale = False
    try:
        current_locks(root, spec)
    except PFBError as exc:
        if state["status"] in {"BUILT", "RUNNING", "STOPPED", "STALE"}:
            stale = True
    project = compose_project(spec["id"])
    running = app_container_running(project)
    result = {
        "id": spec["id"],
        "name": spec["name"],
        "status": "STALE" if stale else ("RUNNING" if running else state["status"]),
        "health": app_container_health(project),
        "url": app_origin(spec["id"]),
        "runtimeOriginTemplate": runtime_origin_template(spec["id"]),
        "git": worktree_identity(root),
    }
    if args.format == "json":
        print(canonical_bytes(result).decode("utf-8"))
    else:
        _result(result)
    return 0


def command_logs(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    return app_logs(root, spec, args.service)


def command_verify(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    _checked_current_locks(root, spec)
    run_id = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    evidence = root / ".pfb/evidence" / run_id
    evidence.mkdir(parents=True, mode=0o700)
    checks = [
        ("ACC-PFB-001", [sys.executable, "scripts/test_pfb.py"]),
        ("ACC-PFB-005", [sys.executable, "scripts/test_pfb_gateway.py"]),
        ("ACC-PFB-011", [
            "docker", "run", "--rm", "--user", f"{os.getuid()}:{os.getgid()}",
            "--env", "GOCACHE=/tmp/go-build", "--env", "GOMODCACHE=/tmp/go-mod",
            "--volume", f"{root}:/workspace", "--workdir", "/workspace",
            "golang:1.26.5-bookworm@sha256:53eeac89074db483fdf0ab3be1df32bf6e47562263d2d0d6baa7f26acb4957dd",
            "go", "test", "./internal/config/...", "./internal/dependencies/...",
            "./internal/rpgmaker/isolation/...",
        ]),
    ]
    results = []
    for case_id, command in checks:
        completed = subprocess.run(command, cwd=root, capture_output=True, text=True, check=False)
        case_root = evidence / "cases" / case_id
        case_root.mkdir(parents=True)
        result = {"caseId": case_id, "status": "PASSED" if completed.returncode == 0 else "FAILED"}
        results.append(result)
        (case_root / "output.log").write_text(completed.stdout + completed.stderr, encoding="utf-8")
        (case_root / "result.json").write_bytes(canonical_bytes(result) + b"\n")
        if completed.returncode != 0:
            raise CommandFailure("PFB_VERIFY_FAILED")
    shutil.copy2(root / ".pfb/locks/candidate-lock.json", evidence / "pfb-lock.json")
    state = load_state(root, spec["id"])
    (evidence / "run.json").write_bytes(canonical_bytes({
        "schemaVersion": 1, "pfbId": spec["id"], "candidateDigest": state["candidateDigest"],
        "dataCompatibilityDigest": state["dataCompatibilityDigest"], "origin": app_origin(spec["id"]),
        "runtimeOriginTemplate": runtime_origin_template(spec["id"]), "cases": results,
    }) + b"\n")
    (evidence / "network.json").write_bytes(canonical_bytes({
        "schemaVersion": 1, "network": "retrom-pfb-gateway-v1",
        "gatewayBind": "127.0.0.1:3000", "publishedAppPorts": [],
    }) + b"\n")
    (evidence / "docker-topology.json").write_bytes(canonical_bytes({
        "schemaVersion": 1, "composeProject": compose_project(spec["id"]),
        "networkAlias": compose_project(spec["id"]), "service": "app",
    }) + b"\n")
    _result({"id": spec["id"], "status": "VERIFIED", "evidence": str(evidence)})
    return 0


def command_prune(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    if args.keep < 0 or args.confirm != spec["id"]:
        raise PFBError("PFB_SPEC_INVALID", "confirmation")
    state = load_state(root, spec["id"])
    removed = remove_pfb_volumes(spec["id"], keep=args.keep, current_data_digest=state["dataCompatibilityDigest"])
    _result({"id": spec["id"], "removedVolumes": removed})
    return 0


def command_destroy(root: Path, args: argparse.Namespace) -> int:
    spec = _named_spec(root, args.pfb)
    if args.confirm != spec["id"]:
        raise PFBError("PFB_SPEC_INVALID", "confirmation")
    _down(root, spec)
    removed = remove_pfb_volumes(spec["id"])
    with locked_registry() as (registry, path):
        registry["pfbs"] = [item for item in registry["pfbs"] if item["id"] != spec["id"]]
        if registry["selectedPfbId"] == spec["id"]:
            registry["selectedPfbId"] = None
        save_registry(path, registry)
    shutil.rmtree(root / ".pfb")
    _result({"id": spec["id"], "status": "DESTROYED", "removedVolumes": removed})
    return 0


def command_gateway_up(root: Path, _args: argparse.Namespace) -> int:
    validate_host(root, require_browser=False)
    result = gateway_up(root)
    _result({"status": "RUNNING", "gateway": result, "url": "http://localhost:3000"})
    return 0


def command_gateway_down(root: Path, _args: argparse.Namespace) -> int:
    gateway_down(root)
    _result({"status": "STOPPED"})
    return 0


def command_entrypoint_check(root: Path, args: argparse.Namespace) -> int:
    spec = load_json(root / ".pfb/spec.json")
    if not isinstance(spec, dict) or spec.get("id") != args.pfb_id or spec.get("hostMode") != "LOCALHOST_SHARED_GATEWAY_V1":
        raise PFBError("PFB_SPEC_INVALID", "entrypoint-id")
    entrypoint_locks(root, spec, Path("/workspace/runtime"))
    return 0


def _build_cores(spec: dict[str, Any], destination: Path) -> None:
    destination.mkdir(parents=True)
    for core in spec["cores"]:
        output = destination / core["id"]
        output.mkdir()
        wrapper = Path(core["root"]) / ".github/rpg-runtime/build-candidate.sh"
        if not wrapper.is_file() or not os.access(wrapper, os.X_OK):
            raise PFBError("PFB_CORE_CANDIDATE_INTERFACE_MISSING", core["id"])
        _checked_run([str(wrapper), str(output)], cwd=Path(core["root"]))
        _validate_core_descriptor(core, output)


def _build_runtime(spec: dict[str, Any], root: Path, destination: Path) -> None:
    destination.mkdir(parents=True)
    if spec["runtime"]["mode"] == "formal":
        return
    run_runtime_candidate_builder(root, spec, destination)
    descriptor = destination / "retrom-runtime-candidate.json"
    if not descriptor.is_file():
        raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", "runtime-descriptor")


def _validate_core_descriptor(core: dict[str, Any], output: Path) -> None:
    descriptor_path = output / "retrom-core-candidate.json"
    value = load_json(descriptor_path, "PFB_CANDIDATE_OUTPUT_INVALID")
    fields = {
        "schemaVersion", "kind", "coreId", "repository", "branch", "commit",
        "dirty", "sourceTreeSha256", "adapterAbi", "files",
    }
    if not isinstance(value, dict) or set(value) != fields or value["schemaVersion"] != 1 or value["kind"] != "RETROM_CORE_CANDIDATE_V1" or value["coreId"] != core["id"]:
        raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", core["id"])
    identity = worktree_identity(Path(core["root"]))
    for field in ("branch", "commit", "dirty", "sourceTreeSha256"):
        if value[field] != identity[field]:
            raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", f"{core['id']}-{field}")
    fork = load_json(Path(core["root"]) / "retrom-fork.json", "PFB_BRANCH_POLICY_INVALID")
    if value["repository"] != fork.get("forkRepository") or value["adapterAbi"] != fork.get("adapterAbi"):
        raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", core["id"])
    names = []
    for item in value["files"] if isinstance(value["files"], list) else []:
        if not isinstance(item, dict) or set(item) != {"filename", "sizeBytes", "sha256"}:
            raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", core["id"])
        target = output / item["filename"]
        if Path(item["filename"]).name != item["filename"] or not target.is_file() or target.is_symlink() or target.stat().st_size != item["sizeBytes"] or sha256_file(target) != item["sha256"]:
            raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", core["id"])
        names.append(item["filename"])
    actual = sorted(path.name for path in output.iterdir() if path.is_file() and path.name != descriptor_path.name)
    if names != sorted(set(names), key=lambda item: item.encode("utf-8")) or actual != names:
        raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", core["id"])


def _validate_branch_policy(spec: dict[str, Any]) -> None:
    for source in [spec["retrom"], *([spec["runtime"]] if spec["runtime"]["mode"] == "branch" else [])]:
        worktree_identity(Path(source["root"]))
    for core in spec["cores"]:
        root = Path(core["root"])
        identity = worktree_identity(root)
        fork = load_json(root / "retrom-fork.json", "PFB_BRANCH_POLICY_INVALID")
        if not isinstance(fork, dict) or fork.get("schemaVersion") != 1 or not isinstance(fork.get("defaultBranch"), str):
            raise PFBError("PFB_BRANCH_POLICY_INVALID", core["id"])
        branch = identity["branch"]
        if not any(branch.startswith(prefix) for prefix in ("fix/", "feat/", "build/", "sync/upstream-")):
            raise PFBError("PFB_BRANCH_POLICY_INVALID", core["id"])
        result = subprocess.run(["git", "-C", str(root), "merge-base", "--is-ancestor", fork["defaultBranch"], "HEAD"], check=False)
        if result.returncode != 0:
            raise PFBError("PFB_BRANCH_POLICY_INVALID", core["id"])


def _require_candidate_outputs(lock: dict[str, Any], spec: dict[str, Any]) -> None:
    if spec["runtime"]["mode"] == "branch" and not lowercase_hex(lock["runtime"].get("candidateSha256"), 64):
        raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", "runtime")
    if not lowercase_hex(lock.get("providerInputSha256"), 64):
        raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", "provider-input")
    if any(not lowercase_hex(item.get("candidateSha256"), 64) for item in lock["cores"]):
        raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", "core")


def _checked_current_locks(root: Path, spec: dict[str, Any]) -> tuple[dict[str, Any], dict[str, Any], str, str]:
    try:
        return current_locks(root, spec)
    except PFBError as exc:
        if exc.code in {"PFB_SOURCE_STALE", "PFB_CANDIDATE_OUTPUT_INVALID", "PFB_SPEC_INVALID"}:
            write_state(root, spec["id"], "STALE", error=str(exc))
            _registry_status(spec["id"], "STALE")
        raise


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


def _wait_healthy(container: str) -> None:
    deadline = time.monotonic() + 300
    while time.monotonic() < deadline:
        health = app_container_health(container)
        if health == "healthy":
            return
        if not app_container_running(container):
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


def _checked_run(arguments: list[str], *, cwd: Path) -> None:
    result = subprocess.run(arguments, cwd=cwd, check=False)
    if result.returncode != 0:
        raise CommandFailure("PFB_BUILD_FAILED")


def _publish_directory(staging: Path, target: Path) -> None:
    backup = target.with_name(f".{target.name}.previous")
    if backup.exists():
        shutil.rmtree(backup)
    if target.exists():
        target.rename(backup)
    try:
        staging.rename(target)
    except OSError:
        if backup.exists() and not target.exists():
            backup.rename(target)
        raise
    shutil.rmtree(backup, ignore_errors=True)


def _result(value: dict[str, Any]) -> None:
    print(json.dumps(value, ensure_ascii=False, sort_keys=True))


if __name__ == "__main__":
    raise SystemExit(main())
