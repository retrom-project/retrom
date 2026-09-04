"""Docker lifecycle for the shared gateway and lightweight PFB dev containers."""

from __future__ import annotations

import ipaddress
import json
import os
import shutil
import socket
import subprocess
from hashlib import sha256
from pathlib import Path
from typing import Any, Callable

from .common import atomic_json, atomic_text, canonical_bytes, load_json, sha256_bytes, sha256_file
from .errors import PFBError
from .identity import compose_project
from .registry import locked_registry, save_registry
from .source_tree import git_common_dir

NETWORK = "retrom-pfb-gateway-v1"
GATEWAY_CONTAINER = NETWORK
DEFAULT_SUBNET = "172.29.240.0/24"
DEV_IMAGE_PREFIX = "retrom-pfb-dev"
COPY_IMAGE = "alpine:3.22.1@sha256:4bcff63911fcb4448bd4fdacec207030997caf25e9bea4045fa6c8c44de311d1"


def gateway_contract(root: Path) -> dict[str, str]:
    gateway_root = root / "scripts/pfb/gateway"
    image = (gateway_root / "image.lock").read_text(encoding="utf-8").strip()
    if "@sha256:" not in image or image.endswith("@sha256:"):
        raise PFBError("PFB_GATEWAY_VERSION_CONFLICT", "image-lock")
    digest = sha256_bytes(canonical_bytes({
        "contractVersion": 1, "image": image,
        "nginx": sha256_file(gateway_root / "nginx.conf"),
        "proxy": sha256_file(gateway_root / "proxy.inc"),
        "compose": sha256_file(gateway_root / "compose.yaml"),
    }))
    subnet = _checked_subnet(os.environ.get("PFB_DOCKER_SUBNET", DEFAULT_SUBNET))
    network = ipaddress.ip_network(subnet)
    return {"contractVersion": "1", "configSha256": digest, "subnet": subnet,
            "gatewayIp": str(network.network_address + 2), "image": image}


def gateway_up(root: Path) -> dict[str, Any]:
    contract = gateway_contract(root)
    with locked_registry() as (registry, path):
        existing = registry["gateway"]
        if existing is not None and existing != _registry_gateway(contract):
            if any(item["status"] == "RUNNING" for item in registry["pfbs"]):
                raise PFBError("PFB_GATEWAY_VERSION_CONFLICT")
        _ensure_gateway_files(root, registry.get("selectedPfbId"))
        if not container_running(GATEWAY_CONTAINER):
            _check_port_available()
        _ensure_network(contract["subnet"])
        environment = _gateway_environment(contract)
        _compose(root / "scripts/pfb/gateway/compose.yaml", ["up", "-d", "--remove-orphans"], environment)
        _verify_gateway_container(contract)
        registry["gateway"] = _registry_gateway(contract)
        save_registry(path, registry)
        return registry["gateway"]


def gateway_preflight(root: Path) -> None:
    contract = gateway_contract(root)
    with locked_registry() as (registry, _path):
        existing = registry["gateway"]
        if existing is not None and existing != _registry_gateway(contract):
            raise PFBError("PFB_GATEWAY_VERSION_CONFLICT")
    if container_running(GATEWAY_CONTAINER):
        _verify_gateway_container(contract)
        return
    _check_port_available()
    inspect = subprocess.run(["docker", "network", "inspect", NETWORK], capture_output=True, text=True, check=False)
    if inspect.returncode == 0:
        try:
            configured = json.loads(inspect.stdout)[0]["IPAM"]["Config"][0]["Subnet"]
        except (IndexError, KeyError, TypeError, json.JSONDecodeError) as exc:
            raise PFBError("PFB_NETWORK_SUBNET_CONFLICT") from exc
        if configured != contract["subnet"]:
            raise PFBError("PFB_NETWORK_SUBNET_CONFLICT")
    else:
        _assert_subnet_available(contract["subnet"])


def gateway_down(root: Path) -> None:
    with locked_registry() as (registry, path):
        if any(item["status"] == "RUNNING" for item in registry["pfbs"]):
            raise PFBError("PFB_GATEWAY_VERSION_CONFLICT", "running-pfb")
        contract = gateway_contract(root)
        _compose(root / "scripts/pfb/gateway/compose.yaml", ["down", "--remove-orphans"],
                 _gateway_environment(contract), allow_failure=True)
        registry["gateway"] = None
        save_registry(path, registry)


def set_selected(root: Path, pfb_id: str | None) -> None:
    _write_selected(pfb_id)
    if container_running(GATEWAY_CONTAINER):
        config = "/etc/nginx/pfb/nginx.conf"
        _run(["docker", "exec", GATEWAY_CONTAINER, "nginx", "-t", "-c", config], "PFB_GATEWAY_VERSION_CONFLICT")
        _run(["docker", "exec", GATEWAY_CONTAINER, "nginx", "-s", "reload", "-c", config],
             "PFB_GATEWAY_VERSION_CONFLICT")


def workspace_paths(root: Path) -> dict[str, Path]:
    workspace = root / ".pfb/workspace"
    return {
        "root": workspace, "data": workspace / "data", "devState": workspace / "dev-state",
        "providers": workspace / "providers", "providerInstalled": workspace / "providers/installed",
        "providerActive": workspace / "providers/active.json", "providerDev": workspace / "providers/dev",
        "webNode": workspace / "web-node", "runtimeNode": workspace / "runtime-node",
        "next": workspace / "next", "go": workspace / "go-cache", "home": workspace / "home",
    }


def ensure_workspace(root: Path) -> dict[str, Path]:
    paths = workspace_paths(root)
    for name, path in paths.items():
        if name != "providerActive":
            path.mkdir(parents=True, exist_ok=True, mode=0o700)
    return paths


def import_provider_base(
    root: Path,
    source_root: Path,
    validate: Callable[[Path, Path], dict[str, Any]],
    verify_upgrade: Callable[[dict[str, Any], dict[str, Any]], None],
) -> dict[str, Any]:
    """Import an already-installed Provider base without archives or network access."""
    paths = ensure_workspace(root)
    try:
        source = source_root.resolve(strict=True)
    except OSError as exc:
        raise PFBError("PFB_PROVIDER_BASE_INVALID", "source-root") from exc
    if not source.is_dir() or source == paths["providers"].resolve():
        raise PFBError("PFB_PROVIDER_BASE_INVALID", "source-root")
    source_active = source / "active.json"
    source_installed = source / "installed"
    if not source_active.is_file() or not source_installed.is_dir():
        raise PFBError("PFB_PROVIDER_BASE_INVALID", "source-layout")
    try:
        incoming = validate(source_active, source_installed)
        if paths["providerActive"].is_file():
            current = validate(paths["providerActive"], paths["providerInstalled"])
            verify_upgrade(current, incoming)
    except (OSError, TypeError, ValueError) as exc:
        raise PFBError("PFB_PROVIDER_BASE_INVALID", str(exc)[:160]) from exc

    staging = root / ".pfb/.providers-importing"
    shutil.rmtree(staging, ignore_errors=True)
    try:
        shutil.copytree(source_installed, staging / "installed", symlinks=True)
        shutil.copy2(source_active, staging / "active.json", follow_symlinks=False)
        staged = validate(staging / "active.json", staging / "installed")
        if canonical_bytes(staged) != canonical_bytes(incoming):
            raise PFBError("PFB_PROVIDER_BASE_INVALID", "active-changed-during-copy")
        installation_paths = _provider_installation_paths(staged)
        for relative in installation_paths:
            staged_bundle = staging / "installed" / relative
            target_bundle = paths["providerInstalled"] / relative
            if not staged_bundle.is_dir():
                raise PFBError("PFB_PROVIDER_BASE_INVALID", "installation-path")
            staged_digest = _directory_digest(staged_bundle)
            if target_bundle.exists() and _directory_digest(target_bundle) != staged_digest:
                raise PFBError("PFB_PROVIDER_BASE_INVALID", f"immutable-conflict-{relative}")
        for relative in installation_paths:
            staged_bundle = staging / "installed" / relative
            target_bundle = paths["providerInstalled"] / relative
            if not target_bundle.exists():
                target_bundle.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                staged_bundle.rename(target_bundle)
        atomic_json(paths["providerActive"], incoming)
        validate(paths["providerActive"], paths["providerInstalled"])
        return {"providerCount": len(installation_paths), "source": incoming["source"]}
    except PFBError:
        raise
    except (OSError, TypeError, ValueError) as exc:
        raise PFBError("PFB_PROVIDER_BASE_INVALID", str(exc)[:160]) from exc
    finally:
        shutil.rmtree(staging, ignore_errors=True)


def _provider_installation_paths(active: dict[str, Any]) -> list[Path]:
    providers = active.get("providers") if isinstance(active, dict) else None
    if not isinstance(providers, list) or not providers:
        raise PFBError("PFB_PROVIDER_BASE_INVALID", "providers")
    result: list[Path] = []
    for provider in providers:
        raw = provider.get("installationPath") if isinstance(provider, dict) else None
        relative = Path(raw) if isinstance(raw, str) else Path()
        if not raw or relative.is_absolute() or ".." in relative.parts or len(relative.parts) != 2:
            raise PFBError("PFB_PROVIDER_BASE_INVALID", "installation-path")
        result.append(relative)
    if len(set(result)) != len(result):
        raise PFBError("PFB_PROVIDER_BASE_INVALID", "duplicate-installation-path")
    return sorted(result, key=lambda item: item.as_posix())


def _directory_digest(root: Path) -> str:
    digest = sha256()
    for path in sorted(root.rglob("*"), key=lambda item: item.relative_to(root).as_posix()):
        relative = path.relative_to(root).as_posix().encode("utf-8")
        if path.is_symlink() or not (path.is_dir() or path.is_file()):
            raise PFBError("PFB_PROVIDER_BASE_INVALID", "special-file")
        digest.update(b"D\0" if path.is_dir() else b"F\0")
        digest.update(relative)
        digest.update(b"\0")
        if path.is_file():
            with path.open("rb") as stream:
                for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                    digest.update(chunk)
    return digest.hexdigest()


def build_toolchain(root: Path, spec: dict[str, Any]) -> dict[str, Any]:
    paths = ensure_workspace(root)
    digest = _toolchain_digest(root)
    image = f"{DEV_IMAGE_PREFIX}:{digest}"
    image_built = subprocess.run(["docker", "image", "inspect", image], capture_output=True, check=False).returncode != 0
    if image_built:
        _run(["docker", "build", "--file", str(root / "scripts/pfb/Dockerfile"),
              "--build-arg", f"PFB_TOOLCHAIN_DIGEST={digest}", "--tag", image, str(root)],
             "PFB_TOOLCHAIN_BUILD_FAILED")
    inputs = _development_inputs_digest(root, spec)
    marker = paths["root"] / "toolchain.json"
    previous = load_json(marker) if marker.is_file() else None
    dependencies_changed = not isinstance(previous, dict) or previous.get("inputsSha256") != inputs
    if dependencies_changed:
        _run_dev_command(root, spec, image, paths, root / "web", ["npm", "ci"])
        _run_dev_command(root, spec, image, paths, Path(spec["runtime"]["root"]), ["npm", "ci"])
        _run_dev_command(root, spec, image, paths, root,
                         ["make", "api-generate-go", "GO_PREPARE_MODE=system", "NODE_PREPARE_MODE=system"])
        atomic_json(marker, {"schemaVersion": 1, "toolchainSha256": digest, "inputsSha256": inputs})
    return {"image": image, "toolchainSha256": digest, "inputsSha256": inputs,
            "imageBuilt": image_built, "dependenciesChanged": dependencies_changed}


def app_up(root: Path, spec: dict[str, Any]) -> None:
    contract = gateway_contract(root)
    _require_toolchain(root, spec)
    _compose(root / "scripts/pfb/compose.yaml", ["up", "-d", "--no-build", "--remove-orphans"],
             _app_environment(root, spec, contract["gatewayIp"]), project=compose_project(spec["id"]))


def app_restart(root: Path, spec: dict[str, Any]) -> None:
    _require_toolchain(root, spec)
    _compose(root / "scripts/pfb/compose.yaml", ["restart", "app"], _minimal_app_environment(root, spec),
             project=compose_project(spec["id"]))


def app_down(root: Path, spec: dict[str, Any]) -> None:
    _compose(root / "scripts/pfb/compose.yaml", ["down", "--remove-orphans"],
             _minimal_app_environment(root, spec), project=compose_project(spec["id"]), allow_failure=True)


def app_logs(root: Path, spec: dict[str, Any], service: str) -> int:
    if service not in {"all", "app"}:
        raise PFBError("PFB_SPEC_INVALID", "service")
    arguments = ["logs", "--follow"] + (["app"] if service == "app" else [])
    return _compose(root / "scripts/pfb/compose.yaml", arguments, _minimal_app_environment(root, spec),
                    project=compose_project(spec["id"]), capture=False, allow_failure=True).returncode


def migrate_legacy_storage(root: Path, spec: dict[str, Any], preferred_data_digest: str | None) -> list[str]:
    paths = workspace_paths(root)
    marker = paths["root"] / "migration.json"
    if marker.is_file():
        value = load_json(marker)
        return list(value.get("legacyVolumes", [])) if isinstance(value, dict) else []
    if paths["root"].exists() and any(paths["root"].iterdir()):
        raise PFBError("PFB_STORAGE_MIGRATION_INVALID", "workspace-not-empty")
    volumes = legacy_volumes(spec["id"])
    selected: dict[str, str] = {}
    for kind in ("data", "node", "runtime-node", "next", "go"):
        candidates = [name for name in volumes if name.startswith(f"{compose_project(spec['id'])}-{kind}-")]
        if kind == "data" and preferred_data_digest:
            preferred = [name for name in candidates if name.endswith(preferred_data_digest[:12])]
            candidates = preferred or candidates
        if candidates:
            selected[kind] = _newest_volume(candidates)
    if "data" not in selected:
        raise PFBError("PFB_STORAGE_MIGRATION_INVALID", "legacy-data-volume-missing")
    staging = root / ".pfb/.workspace-migrating"
    if staging.exists():
        shutil.rmtree(staging)
    staging.mkdir(mode=0o700)
    targets = {"data": staging, "node": staging / "web-node", "runtime-node": staging / "runtime-node",
               "next": staging / "next", "go": staging / "go-cache"}
    try:
        for kind, volume in selected.items():
            targets[kind].mkdir(parents=True, exist_ok=True, mode=0o700)
            _run(["docker", "run", "--rm", "--volume", f"{volume}:/legacy:ro",
                  "--volume", f"{targets[kind]}:/target", COPY_IMAGE, "sh", "-c", "cp -a /legacy/. /target/"],
                 "PFB_STORAGE_MIGRATION_INVALID")
            if _tree_fingerprint(f"{volume}:/tree:ro") != _tree_fingerprint(f"{targets[kind]}:/tree:ro"):
                raise PFBError("PFB_STORAGE_MIGRATION_INVALID", f"copy-mismatch-{kind}")
        for relative in ("data", "dev-state", "providers/dev", "providers/installed", "home"):
            (staging / relative).mkdir(parents=True, exist_ok=True, mode=0o700)
        atomic_json(staging / "migration.json", {
            "schemaVersion": 1, "pfbId": spec["id"], "legacyVolumes": sorted(selected.values()),
        })
        if paths["root"].exists():
            paths["root"].rmdir()
        staging.rename(paths["root"])
    finally:
        shutil.rmtree(staging, ignore_errors=True)
    return sorted(selected.values())


def legacy_volumes(pfb_id: str) -> list[str]:
    prefix = f"{compose_project(pfb_id)}-"
    return sorted(name for name in _run_json(["docker", "volume", "ls", "--format", "{{json .Name}}"])
                  if isinstance(name, str) and name.startswith(prefix))


def container_running(name: str) -> bool:
    result = subprocess.run(["docker", "inspect", "--format", "{{.State.Running}}", name],
                            capture_output=True, text=True, check=False)
    return result.returncode == 0 and result.stdout.strip() == "true"


def container_health(name: str) -> str:
    result = subprocess.run(["docker", "inspect", "--format",
                             "{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}", name],
                            capture_output=True, text=True, check=False)
    return result.stdout.strip() if result.returncode == 0 else "absent"


def app_container_running(project: str) -> bool:
    container = _compose_service_container(project)
    return container is not None and container_running(container)


def app_container_health(project: str) -> str:
    container = _compose_service_container(project)
    return container_health(container) if container is not None else "absent"


def _compose_service_container(project: str) -> str | None:
    result = subprocess.run(["docker", "container", "ls", "--all", "--quiet",
                             "--filter", f"label=com.docker.compose.project={project}",
                             "--filter", "label=com.docker.compose.service=app",
                             "--filter", "label=com.docker.compose.oneoff=False"],
                            capture_output=True, text=True, check=False)
    if result.returncode != 0:
        return None
    identifiers = [line.strip() for line in result.stdout.splitlines() if line.strip()]
    return identifiers[0] if len(identifiers) == 1 else None


def _app_environment(root: Path, spec: dict[str, Any], gateway_ip: str) -> dict[str, str]:
    if spec["runtime"]["mode"] != "branch":
        raise PFBError("PFB_SPEC_INVALID", "runtime-worktree-required")
    generated = _prepare_generated_files(root)
    return {**_minimal_app_environment(root, spec), "PFB_NEXT_ENV_FILE": str(generated / "next-env.d.ts"),
            "PFB_TSCONFIG_FILE": str(generated / "tsconfig.json"), "PFB_GATEWAY_IP": gateway_ip}


def _minimal_app_environment(root: Path, spec: dict[str, Any]) -> dict[str, str]:
    generated = _prepare_generated_files(root)
    runtime_root = Path(spec["runtime"].get("root", root / ".pfb/formal-runtime"))
    formal_git = generated / "formal-runtime-git"
    formal_git.mkdir(exist_ok=True)
    runtime_git = git_common_dir(runtime_root) if spec["runtime"]["mode"] == "branch" else formal_git
    return {
        "PFB_RETROM_ROOT": str(root), "PFB_RETROM_GIT_COMMON_DIR": str(git_common_dir(root)),
        "PFB_RUNTIME_ROOT": str(runtime_root), "PFB_RUNTIME_GIT_COMMON_DIR": str(runtime_git),
        "PFB_NEXT_ENV_FILE": str(generated / "next-env.d.ts"), "PFB_TSCONFIG_FILE": str(generated / "tsconfig.json"),
        "PFB_WORKSPACE_ROOT": str(workspace_paths(root)["root"]), "PFB_ID": spec["id"],
        "PFB_GATEWAY_IP": "172.29.240.2", "PFB_UID": str(os.getuid()), "PFB_GID": str(os.getgid()),
        "PFB_TOOLCHAIN_DIGEST": _toolchain_digest(root),
    }


def _toolchain_digest(root: Path) -> str:
    return sha256_bytes(canonical_bytes({
        "dockerfile": sha256_file(root / "scripts/pfb/Dockerfile"),
        "entrypoint": sha256_file(root / "scripts/pfb/entrypoint.sh"),
        "compose": sha256_file(root / "scripts/pfb/compose.yaml"),
        "node": (root / ".node-version").read_text(encoding="utf-8").strip(),
        "go": (root / "go.mod").read_text(encoding="utf-8").splitlines()[2],
    }))


def _development_inputs_digest(root: Path, spec: dict[str, Any]) -> str:
    if spec["runtime"]["mode"] != "branch":
        raise PFBError("PFB_SPEC_INVALID", "runtime-worktree-required")
    runtime_root = Path(spec["runtime"]["root"])
    api_bytes = b"".join(path.read_bytes() for path in sorted((root / "api").rglob("*.yaml")))
    return sha256_bytes(canonical_bytes({
        "webPackage": sha256_file(root / "web/package.json"), "webLock": sha256_file(root / "web/package-lock.json"),
        "runtimePackage": sha256_file(runtime_root / "package.json"),
        "runtimeLock": sha256_file(runtime_root / "package-lock.json"),
        "goMod": sha256_file(root / "go.mod"), "goSum": sha256_file(root / "go.sum"), "api": sha256_bytes(api_bytes),
    }))


def _require_toolchain(root: Path, spec: dict[str, Any]) -> None:
    digest = _toolchain_digest(root)
    if subprocess.run(["docker", "image", "inspect", f"{DEV_IMAGE_PREFIX}:{digest}"],
                      capture_output=True, check=False).returncode != 0:
        raise PFBError("PFB_TOOLCHAIN_MISSING", "run-pfb-build")
    marker = workspace_paths(root)["root"] / "toolchain.json"
    value = load_json(marker) if marker.is_file() else None
    if not isinstance(value, dict) or value.get("toolchainSha256") != digest or \
            value.get("inputsSha256") != _development_inputs_digest(root, spec):
        raise PFBError("PFB_TOOLCHAIN_STALE", "run-pfb-build")
    if not workspace_paths(root)["providerActive"].is_file():
        raise PFBError("PFB_PROVIDER_BASE_MISSING", "run-pfb-migrate-storage")


def _run_dev_command(root: Path, spec: dict[str, Any], image: str, paths: dict[str, Path], workdir: Path,
                     command: list[str]) -> None:
    runtime_root = Path(spec["runtime"]["root"])
    common = git_common_dir(root)
    arguments = ["docker", "run", "--rm", "--entrypoint", "", "--user", f"{os.getuid()}:{os.getgid()}",
                 "--workdir", str(workdir), "--env", f"HOME={paths['home']}",
                 "--env", f"GOCACHE={paths['go'] / 'build'}", "--env", f"GOMODCACHE={paths['go'] / 'mod'}",
                 "--volume", f"{root}:{root}", "--volume", f"{common}:{common}:ro",
                 "--volume", f"{runtime_root}:{runtime_root}", *_runtime_git_mount_arguments(root, runtime_root),
                 "--volume", f"{paths['webNode']}:{root / 'web/node_modules'}",
                 "--volume", f"{paths['runtimeNode']}:{runtime_root / 'node_modules'}",
                 "--volume", f"{paths['go']}:{paths['go']}", "--volume", f"{paths['home']}:{paths['home']}",
                 image, *command]
    _run(arguments, "PFB_TOOLCHAIN_BUILD_FAILED")


def _runtime_git_mount_arguments(root: Path, runtime_root: Path) -> list[str]:
    common = git_common_dir(runtime_root)
    if common.is_relative_to(root) or common.is_relative_to(runtime_root):
        return []
    return ["--volume", f"{common}:{common}:ro"]


def _prepare_generated_files(root: Path) -> Path:
    generated = root / ".pfb/generated"
    generated.mkdir(parents=True, exist_ok=True, mode=0o700)
    for name in ("next-env.d.ts", "tsconfig.json"):
        shutil.copy2(root / "web" / name, generated / name)
    return generated


def _newest_volume(names: list[str]) -> str:
    records = []
    for name in names:
        inspected = _run_json(["docker", "volume", "inspect", name])
        if len(inspected) != 1 or not isinstance(inspected[0].get("CreatedAt"), str):
            raise PFBError("PFB_STORAGE_MIGRATION_INVALID", "volume-created-at")
        records.append((inspected[0]["CreatedAt"], name))
    return max(records)[1]


def _tree_fingerprint(mount: str) -> str:
    output = _run(["docker", "run", "--rm", "--volume", mount, COPY_IMAGE, "sh", "-c",
                   "cd /tree && (find . -xdev -type f -exec sha256sum {} ';'; "
                   "find . -xdev -type l -exec sh -c 'printf \"L  %s  %s\\n\" \"$1\" \"$(readlink \"$1\")\"' sh {} ';') "
                   "| LC_ALL=C sort | sha256sum"], "PFB_STORAGE_MIGRATION_INVALID")
    value = output.split()[0] if output.split() else ""
    if len(value) != 64 or any(character not in "0123456789abcdef" for character in value):
        raise PFBError("PFB_STORAGE_MIGRATION_INVALID", "fingerprint")
    return value


def _gateway_environment(contract: dict[str, str]) -> dict[str, str]:
    return {"PFB_GATEWAY_IMAGE": contract["image"], "PFB_GATEWAY_CONFIG_DIR": str(_gateway_state_dir()),
            "PFB_GATEWAY_IP": contract["gatewayIp"], "PFB_GATEWAY_CONFIG_SHA256": contract["configSha256"],
            "PFB_UID": str(os.getuid()), "PFB_GID": str(os.getgid())}


def _ensure_gateway_files(root: Path, selected: str | None) -> None:
    directory = _gateway_state_dir()
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(directory, 0o700)
    atomic_text(directory / "nginx.conf", (root / "scripts/pfb/gateway/nginx.conf").read_text(encoding="utf-8"))
    atomic_text(directory / "proxy.inc", (root / "scripts/pfb/gateway/proxy.inc").read_text(encoding="utf-8"))
    _write_selected(selected)


def _write_selected(selected: str | None) -> None:
    value = f"http://{selected}.localhost:3000" if selected else ""
    atomic_text(_gateway_state_dir() / "selected.conf", f'set $pfb_selected_origin "{value}";\n')


def _gateway_state_dir() -> Path:
    from .registry import state_root
    return state_root() / "gateway-v1"


def _registry_gateway(contract: dict[str, str]) -> dict[str, Any]:
    return {"contractVersion": 1, "configSha256": contract["configSha256"], "subnet": contract["subnet"],
            "gatewayIp": contract["gatewayIp"], "image": contract["image"]}


def _checked_subnet(raw: str) -> str:
    try:
        value = ipaddress.ip_network(raw, strict=True)
    except ValueError as exc:
        raise PFBError("PFB_NETWORK_SUBNET_CONFLICT") from exc
    if value.version != 4 or value.prefixlen != 24 or not value.is_private:
        raise PFBError("PFB_NETWORK_SUBNET_CONFLICT")
    return str(value)


def _ensure_network(subnet: str) -> None:
    inspect = subprocess.run(["docker", "network", "inspect", NETWORK], capture_output=True, text=True, check=False)
    if inspect.returncode == 0:
        if json.loads(inspect.stdout)[0]["IPAM"]["Config"][0]["Subnet"] != subnet:
            raise PFBError("PFB_NETWORK_SUBNET_CONFLICT")
        return
    _assert_subnet_available(subnet)
    _run(["docker", "network", "create", "--driver", "bridge", "--subnet", subnet, NETWORK],
         "PFB_NETWORK_SUBNET_CONFLICT")


def _assert_subnet_available(subnet: str) -> None:
    candidate = ipaddress.ip_network(subnet)
    identifiers = _run(["docker", "network", "ls", "--quiet"], "PFB_NETWORK_SUBNET_CONFLICT").split()
    if identifiers:
        for network in _run_json(["docker", "network", "inspect", *identifiers]):
            for configuration in network.get("IPAM", {}).get("Config", []):
                raw = configuration.get("Subnet")
                try:
                    existing = ipaddress.ip_network(raw, strict=False) if isinstance(raw, str) else None
                except ValueError:
                    existing = None
                if existing is not None and candidate.overlaps(existing):
                    raise PFBError("PFB_NETWORK_SUBNET_CONFLICT", raw)
    try:
        route = subprocess.run(["ip", "-j", "-4", "route", "show"], capture_output=True, text=True, check=False)
    except FileNotFoundError as exc:
        raise PFBError("PFB_TOOLCHAIN_MISSING", "ip") from exc
    if route.returncode == 0:
        for item in json.loads(route.stdout):
            raw = item.get("dst")
            try:
                existing = ipaddress.ip_network(raw, strict=False) if isinstance(raw, str) and raw != "default" else None
            except ValueError:
                existing = None
            if existing is not None and candidate.overlaps(existing):
                raise PFBError("PFB_NETWORK_SUBNET_CONFLICT", raw)


def _check_port_available() -> None:
    sockets = []
    try:
        for family, address in ((socket.AF_INET, ("127.0.0.1", 3000)), (socket.AF_INET6, ("::1", 3000))):
            candidate = socket.socket(family, socket.SOCK_STREAM)
            sockets.append(candidate)
            candidate.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 0)
            candidate.bind(address)
    except OSError as exc:
        raise PFBError("PFB_GATEWAY_PORT_IN_USE", "127.0.0.1:3000") from exc
    finally:
        for candidate in sockets:
            candidate.close()


def _verify_gateway_container(contract: dict[str, str]) -> None:
    inspect = _run_json(["docker", "inspect", GATEWAY_CONTAINER])
    if len(inspect) != 1:
        raise PFBError("PFB_GATEWAY_VERSION_CONFLICT")
    item = inspect[0]
    bindings = item["HostConfig"]["PortBindings"].get("3000/tcp", [])
    if bindings != [{"HostIp": "127.0.0.1", "HostPort": "3000"}] or len(item["HostConfig"]["PortBindings"]) != 1:
        raise PFBError("PFB_GATEWAY_BIND_INVALID")
    labels = item["Config"]["Labels"]
    if labels.get("io.retrom.pfb-contract-version") != "1" or \
            labels.get("io.retrom.pfb-config-sha256") != contract["configSha256"]:
        raise PFBError("PFB_GATEWAY_VERSION_CONFLICT")
    if item["Config"].get("User") != f"{os.getuid()}:{os.getgid()}":
        raise PFBError("PFB_GATEWAY_USER_INVALID")


def _compose(file: Path, arguments: list[str], environment: dict[str, str], *, project: str | None = None,
             capture: bool = True, allow_failure: bool = False) -> subprocess.CompletedProcess[str]:
    command = ["docker", "compose", "--file", str(file)]
    if project is not None:
        command.extend(["--project-name", project])
    command.extend(arguments)
    result = subprocess.run(command, env={**os.environ, **environment}, capture_output=capture, text=True, check=False)
    if result.returncode != 0 and not allow_failure:
        raise PFBError("PFB_DOCKER_FAILED", "docker-compose")
    return result


def _run(arguments: list[str], code: str) -> str:
    result = subprocess.run(arguments, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        lines = [line.strip() for line in (result.stderr + "\n" + result.stdout).splitlines() if line.strip()]
        errors = [line for line in lines if "Error:" in line or line.startswith("PFB_")]
        detail = ((errors[-1] if errors else lines[-1]) if lines else arguments[-1])[:240]
        raise PFBError(code, detail)
    return result.stdout


def _run_json(arguments: list[str]) -> Any:
    output = _run(arguments, "PFB_DOCKER_FAILED")
    try:
        if arguments[1:3] == ["volume", "ls"]:
            return [json.loads(line) for line in output.splitlines() if line]
        return json.loads(output)
    except json.JSONDecodeError as exc:
        raise PFBError("PFB_DOCKER_FAILED", "docker-json") from exc
