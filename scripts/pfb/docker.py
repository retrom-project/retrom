"""Docker lifecycle for the shared gateway and isolated PFB applications."""

from __future__ import annotations

import ipaddress
import json
import os
import shutil
import socket
import subprocess
from pathlib import Path
from typing import Any

from .common import atomic_text, canonical_bytes, sha256_bytes, sha256_file
from .errors import PFBError
from .identity import compose_project, network_alias, volume_name
from .registry import locked_registry, save_registry
from .source_tree import git_common_dir


NETWORK = "retrom-pfb-gateway-v1"
GATEWAY_CONTAINER = NETWORK
DEFAULT_SUBNET = "172.29.240.0/24"


def gateway_contract(root: Path) -> dict[str, str]:
    gateway_root = root / "scripts/pfb/gateway"
    image = (gateway_root / "image.lock").read_text(encoding="utf-8").strip()
    if "@sha256:" not in image or image.endswith("@sha256:"):
        raise PFBError("PFB_GATEWAY_VERSION_CONFLICT", "image-lock")
    digest = sha256_bytes(canonical_bytes({
        "contractVersion": 1,
        "image": image,
        "nginx": sha256_file(gateway_root / "nginx.conf"),
        "proxy": sha256_file(gateway_root / "proxy.inc"),
        "compose": sha256_file(gateway_root / "compose.yaml"),
    }))
    subnet = _checked_subnet(os.environ.get("PFB_DOCKER_SUBNET", DEFAULT_SUBNET))
    network = ipaddress.ip_network(subnet)
    return {
        "contractVersion": "1",
        "configSha256": digest,
        "subnet": subnet,
        "gatewayIp": str(network.network_address + 2),
        "image": image,
    }


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
        environment = {
            "PFB_GATEWAY_IMAGE": contract["image"],
            "PFB_GATEWAY_CONFIG_DIR": str(_gateway_state_dir()),
            "PFB_GATEWAY_IP": contract["gatewayIp"],
            "PFB_GATEWAY_CONFIG_SHA256": contract["configSha256"],
            "PFB_UID": str(os.getuid()),
            "PFB_GID": str(os.getgid()),
        }
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
    else:
        _check_port_available()
    inspect = subprocess.run(
        ["docker", "network", "inspect", NETWORK], capture_output=True, text=True, check=False,
    )
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
        environment = {
            "PFB_GATEWAY_IMAGE": contract["image"],
            "PFB_GATEWAY_CONFIG_DIR": str(_gateway_state_dir()),
            "PFB_GATEWAY_IP": contract["gatewayIp"],
            "PFB_GATEWAY_CONFIG_SHA256": contract["configSha256"],
            "PFB_UID": str(os.getuid()),
            "PFB_GID": str(os.getgid()),
        }
        _compose(root / "scripts/pfb/gateway/compose.yaml", ["down", "--remove-orphans"], environment, allow_failure=True)
        registry["gateway"] = None
        save_registry(path, registry)


def set_selected(root: Path, pfb_id: str | None) -> None:
    _write_selected(pfb_id)
    if container_running(GATEWAY_CONTAINER):
        config = "/etc/nginx/pfb/nginx.conf"
        _run(["docker", "exec", GATEWAY_CONTAINER, "nginx", "-t", "-c", config], "PFB_GATEWAY_VERSION_CONFLICT")
        _run(["docker", "exec", GATEWAY_CONTAINER, "nginx", "-s", "reload", "-c", config], "PFB_GATEWAY_VERSION_CONFLICT")


def app_up(root: Path, spec: dict[str, Any], data_digest: str, *, restart: bool = False) -> None:
    contract = gateway_contract(root)
    toolchain_digest = _toolchain_digest(root)
    environment = _app_environment(root, spec, data_digest, toolchain_digest, contract["gatewayIp"])
    _ensure_volume_owners([
        environment["PFB_DATA_VOLUME"], environment["PFB_NODE_VOLUME"],
        environment["PFB_RUNTIME_NODE_VOLUME"], environment["PFB_GO_VOLUME"],
        environment["PFB_NEXT_VOLUME"],
    ], os.getuid(), os.getgid())
    arguments = ["up", "-d", "--build", "--remove-orphans"]
    if restart:
        arguments.extend(["--force-recreate"])
    _compose(root / "scripts/pfb/compose.yaml", arguments, environment, project=compose_project(spec["id"]))


def app_down(root: Path, spec: dict[str, Any]) -> None:
    environment = _minimal_app_environment(root, spec)
    _compose(
        root / "scripts/pfb/compose.yaml", ["down", "--remove-orphans"], environment,
        project=compose_project(spec["id"]), allow_failure=True,
    )


def app_logs(root: Path, spec: dict[str, Any], service: str) -> int:
    if service not in {"all", "app"}:
        raise PFBError("PFB_SPEC_INVALID", "service")
    arguments = ["logs", "--follow"]
    if service == "app":
        arguments.append("app")
    result = _compose(
        root / "scripts/pfb/compose.yaml", arguments, _minimal_app_environment(root, spec),
        project=compose_project(spec["id"]), capture=False, allow_failure=True,
    )
    return result.returncode


def run_runtime_candidate_builder(root: Path, spec: dict[str, Any], output: Path) -> None:
    runtime_root = Path(spec["runtime"]["root"])
    toolchain_digest = _toolchain_digest(root)
    node_volume = volume_name(spec["id"], "runtime-node", toolchain_digest)
    _ensure_volume_owners([node_volume], os.getuid(), os.getgid())
    image = f"retrom-pfb-dev:{toolchain_digest}"
    _run([
        "docker", "build", "--file", str(root / "scripts/pfb/Dockerfile"),
        "--build-arg", f"PFB_TOOLCHAIN_DIGEST={toolchain_digest}",
        "--tag", image, str(root),
    ], "PFB_CANDIDATE_OUTPUT_INVALID")
    base = [
        "docker", "run", "--rm", "--entrypoint", "",
        "--user", f"{os.getuid()}:{os.getgid()}",
        "--workdir", str(runtime_root),
        "--env", f"HOME={runtime_root}",
        "--env", f"npm_config_cache={runtime_root}/node_modules/.npm-cache",
        "--volume", f"{root}:{root}",
        "--volume", f"{runtime_root}:{runtime_root}",
        *_runtime_git_mount_arguments(root, runtime_root),
        "--volume", f"{node_volume}:{runtime_root}/node_modules",
        image,
    ]
    _run([*base, "npm", "ci"], "PFB_CANDIDATE_OUTPUT_INVALID")
    _run([
        *base, "npm", "run", "candidate:build", "--",
        "--spec", str(root / ".pfb/spec.json"), "--output", str(output),
    ], "PFB_CANDIDATE_OUTPUT_INVALID")


def _runtime_git_mount_arguments(root: Path, runtime_root: Path) -> list[str]:
    common = git_common_dir(runtime_root)
    if common.is_relative_to(root) or common.is_relative_to(runtime_root):
        return []
    return ["--volume", f"{common}:{common}:ro"]


def container_running(name: str) -> bool:
    result = subprocess.run(
        ["docker", "inspect", "--format", "{{.State.Running}}", name],
        capture_output=True, text=True, check=False,
    )
    return result.returncode == 0 and result.stdout.strip() == "true"


def container_health(name: str) -> str:
    result = subprocess.run(
        ["docker", "inspect", "--format", "{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}", name],
        capture_output=True, text=True, check=False,
    )
    return result.stdout.strip() if result.returncode == 0 else "absent"


def app_container_running(project: str) -> bool:
    container = _compose_service_container(project)
    return container is not None and container_running(container)


def app_container_health(project: str) -> str:
    container = _compose_service_container(project)
    return container_health(container) if container is not None else "absent"


def _compose_service_container(project: str) -> str | None:
    result = subprocess.run(
        [
            "docker", "container", "ls", "--all", "--quiet",
            "--filter", f"label=com.docker.compose.project={project}",
            "--filter", "label=com.docker.compose.service=app",
            "--filter", "label=com.docker.compose.oneoff=False",
        ],
        capture_output=True, text=True, check=False,
    )
    if result.returncode != 0:
        return None
    identifiers = [line.strip() for line in result.stdout.splitlines() if line.strip()]
    return identifiers[0] if len(identifiers) == 1 else None


def remove_pfb_volumes(pfb_id: str, *, keep: int = 0, current_data_digest: str | None = None) -> list[str]:
    prefix = f"{compose_project(pfb_id)}-"
    result = _run_json(["docker", "volume", "ls", "--format", "{{json .Name}}"])
    names = sorted(item for item in result if isinstance(item, str) and item.startswith(prefix))
    if current_data_digest is not None:
        names = [name for name in names if name.startswith(f"{prefix}data-")]
        protected = {volume_name(pfb_id, "data", current_data_digest)}
        records = []
        for name in names:
            inspected = _run_json(["docker", "volume", "inspect", name])
            if len(inspected) != 1 or not isinstance(inspected[0].get("CreatedAt"), str):
                raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", "volume-created-at")
            records.append((inspected[0]["CreatedAt"], name))
        newest = [name for _created, name in sorted(records, reverse=True)]
        protected.update(newest[:keep])
        removable = [name for name in newest if name not in protected]
    else:
        removable = names
    for name in removable:
        _run(["docker", "volume", "rm", name], "PFB_CANDIDATE_OUTPUT_INVALID")
    return removable


def _app_environment(root: Path, spec: dict[str, Any], data_digest: str, toolchain_digest: str, gateway_ip: str) -> dict[str, str]:
    generated = _prepare_generated_files(root)
    runtime_root = spec["runtime"].get("root") if spec["runtime"]["mode"] == "branch" else str(root / ".pfb/formal-runtime")
    Path(runtime_root).mkdir(parents=True, exist_ok=True) if spec["runtime"]["mode"] == "formal" else None
    return {
        **_minimal_app_environment(root, spec),
        "PFB_NEXT_ENV_FILE": str(generated / "next-env.d.ts"),
        "PFB_TSCONFIG_FILE": str(generated / "tsconfig.json"),
        "PFB_RUNTIME_ROOT": runtime_root,
        "PFB_RUNTIME_MODE": spec["runtime"]["mode"],
        "PFB_DATA_VOLUME": volume_name(spec["id"], "data", data_digest),
        "PFB_NODE_VOLUME": volume_name(spec["id"], "node", toolchain_digest),
        "PFB_RUNTIME_NODE_VOLUME": volume_name(spec["id"], "runtime-node", toolchain_digest),
        "PFB_GO_VOLUME": volume_name(spec["id"], "go", toolchain_digest),
        "PFB_NEXT_VOLUME": volume_name(spec["id"], "next", toolchain_digest),
        "PFB_GATEWAY_IP": gateway_ip,
        "PFB_UID": str(os.getuid()),
        "PFB_GID": str(os.getgid()),
        "PFB_TOOLCHAIN_DIGEST": toolchain_digest,
    }


def _minimal_app_environment(root: Path, spec: dict[str, Any]) -> dict[str, str]:
    generated = _prepare_generated_files(root)
    formal_runtime_git = generated / "formal-runtime-git"
    formal_runtime_git.mkdir(exist_ok=True)
    runtime_git_common = git_common_dir(Path(spec["runtime"]["root"])) \
        if spec["runtime"]["mode"] == "branch" else formal_runtime_git
    empty = "0" * 64
    return {
        "PFB_RETROM_ROOT": str(root),
        "PFB_RETROM_GIT_COMMON_DIR": str(git_common_dir(root)),
        "PFB_RUNTIME_GIT_COMMON_DIR": str(runtime_git_common),
        "PFB_NEXT_ENV_FILE": str(generated / "next-env.d.ts"),
        "PFB_TSCONFIG_FILE": str(generated / "tsconfig.json"),
        "PFB_RUNTIME_ROOT": str(root / ".pfb/formal-runtime"),
        "PFB_RUNTIME_GIT_COMMON_DIR": str(runtime_git_common),
        "PFB_RUNTIME_MODE": spec["runtime"]["mode"],
        "PFB_ID": spec["id"],
        "PFB_DATA_VOLUME": volume_name(spec["id"], "data", empty),
        "PFB_NODE_VOLUME": volume_name(spec["id"], "node", empty),
        "PFB_RUNTIME_NODE_VOLUME": volume_name(spec["id"], "runtime-node", empty),
        "PFB_GO_VOLUME": volume_name(spec["id"], "go", empty),
        "PFB_NEXT_VOLUME": volume_name(spec["id"], "next", empty),
        "PFB_GATEWAY_IP": "172.29.240.2",
        "PFB_UID": str(os.getuid()),
        "PFB_GID": str(os.getgid()),
        "PFB_TOOLCHAIN_DIGEST": empty,
    }


def _toolchain_digest(root: Path) -> str:
    return sha256_bytes(canonical_bytes({
        "dockerfile": sha256_file(root / "scripts/pfb/Dockerfile"),
        "entrypoint": sha256_file(root / "scripts/pfb/entrypoint.sh"),
        "node": (root / ".node-version").read_text(encoding="utf-8").strip(),
        "go": (root / "go.mod").read_text(encoding="utf-8").splitlines()[2],
    }))


def _prepare_generated_files(root: Path) -> Path:
    generated = root / ".pfb/generated"
    generated.mkdir(parents=True, exist_ok=True, mode=0o700)
    for name in ("next-env.d.ts", "tsconfig.json"):
        target = generated / name
        shutil.copy2(root / "web" / name, target)
    return generated


def _ensure_gateway_files(root: Path, selected: str | None) -> None:
    directory = _gateway_state_dir()
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(directory, 0o700)
    atomic_text(directory / "nginx.conf", (root / "scripts/pfb/gateway/nginx.conf").read_text(encoding="utf-8"))
    atomic_text(directory / "proxy.inc", (root / "scripts/pfb/gateway/proxy.inc").read_text(encoding="utf-8"))
    _write_selected(selected)


def _write_selected(selected: str | None) -> None:
    value = f'http://{selected}.localhost:3000' if selected else ""
    atomic_text(_gateway_state_dir() / "selected.conf", f'set $pfb_selected_origin "{value}";\n')


def _gateway_state_dir() -> Path:
    from .registry import state_root
    return state_root() / "gateway-v1"


def _registry_gateway(contract: dict[str, str]) -> dict[str, Any]:
    return {
        "contractVersion": 1,
        "configSha256": contract["configSha256"],
        "subnet": contract["subnet"],
        "gatewayIp": contract["gatewayIp"],
        "image": contract["image"],
    }


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
        value = json.loads(inspect.stdout)[0]
        configured = value["IPAM"]["Config"][0]["Subnet"]
        if configured != subnet:
            raise PFBError("PFB_NETWORK_SUBNET_CONFLICT")
        return
    _assert_subnet_available(subnet)
    _run(["docker", "network", "create", "--driver", "bridge", "--subnet", subnet, NETWORK], "PFB_NETWORK_SUBNET_CONFLICT")


def _assert_subnet_available(subnet: str) -> None:
    candidate = ipaddress.ip_network(subnet)
    identifiers = _run(["docker", "network", "ls", "--quiet"], "PFB_NETWORK_SUBNET_CONFLICT").split()
    if identifiers:
        for network in _run_json(["docker", "network", "inspect", *identifiers]):
            for configuration in network.get("IPAM", {}).get("Config", []):
                raw = configuration.get("Subnet")
                if not isinstance(raw, str):
                    continue
                try:
                    existing = ipaddress.ip_network(raw, strict=False)
                except ValueError:
                    continue
                if candidate.overlaps(existing):
                    raise PFBError("PFB_NETWORK_SUBNET_CONFLICT", raw)
    try:
        route = subprocess.run(
            ["ip", "-j", "-4", "route", "show"], capture_output=True, text=True, check=False,
        )
    except FileNotFoundError as exc:
        raise PFBError("PFB_TOOLCHAIN_MISSING", "ip") from exc
    if route.returncode != 0:
        return
    try:
        routes = json.loads(route.stdout)
    except json.JSONDecodeError as exc:
        raise PFBError("PFB_NETWORK_SUBNET_CONFLICT", "host-routes") from exc
    for item in routes:
        raw = item.get("dst")
        if not isinstance(raw, str) or raw == "default":
            continue
        try:
            existing = ipaddress.ip_network(raw, strict=False)
        except ValueError:
            continue
        if candidate.overlaps(existing):
            raise PFBError("PFB_NETWORK_SUBNET_CONFLICT", raw)


def _ensure_volume_owners(names: list[str], uid: int, gid: int) -> None:
    image = "alpine:3.22.1@sha256:4bcff63911fcb4448bd4fdacec207030997caf25e9bea4045fa6c8c44de311d1"
    for name in names:
        inspect = subprocess.run(["docker", "volume", "inspect", name], capture_output=True, check=False)
        if inspect.returncode != 0:
            _run(["docker", "volume", "create", name], "PFB_CANDIDATE_OUTPUT_INVALID")
        _run([
            "docker", "run", "--rm", "--volume", f"{name}:/volume", image,
            "chown", f"{uid}:{gid}", "/volume",
        ], "PFB_CANDIDATE_OUTPUT_INVALID")


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
    ports = item["HostConfig"]["PortBindings"]
    bindings = ports.get("3000/tcp", [])
    if bindings != [{"HostIp": "127.0.0.1", "HostPort": "3000"}] or len(ports) != 1:
        raise PFBError("PFB_GATEWAY_BIND_INVALID")
    labels = item["Config"]["Labels"]
    if labels.get("io.retrom.pfb-contract-version") != "1" or labels.get("io.retrom.pfb-config-sha256") != contract["configSha256"]:
        raise PFBError("PFB_GATEWAY_VERSION_CONFLICT")
    if item["Config"].get("User") != f"{os.getuid()}:{os.getgid()}":
        raise PFBError("PFB_GATEWAY_USER_INVALID")


def _compose(
    file: Path,
    arguments: list[str],
    environment: dict[str, str],
    *,
    project: str | None = None,
    capture: bool = True,
    allow_failure: bool = False,
) -> subprocess.CompletedProcess[str]:
    command = ["docker", "compose", "--file", str(file)]
    if project is not None:
        command.extend(["--project-name", project])
    command.extend(arguments)
    result = subprocess.run(
        command, env={**os.environ, **environment}, capture_output=capture,
        text=True, check=False,
    )
    if result.returncode != 0 and not allow_failure:
        raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", "docker-compose")
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
    output = _run(arguments, "PFB_CANDIDATE_OUTPUT_INVALID")
    try:
        if arguments[1:3] == ["volume", "ls"]:
            return [json.loads(line) for line in output.splitlines() if line]
        return json.loads(output)
    except json.JSONDecodeError as exc:
        raise PFBError("PFB_CANDIDATE_OUTPUT_INVALID", "docker-json") from exc
