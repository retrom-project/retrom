"""Publish a complete, immutable runtime host-tool snapshot during explicit PFB build."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import tempfile
from .errors import PFBError


def publish_runtime_tool(runtime: Path, workspace: Path, *, self_contained: bool = False) -> str:
    package = json.loads((runtime / "package.json").read_text(encoding="utf-8"))
    inputs = ["package.json", *package["files"]]
    if self_contained:
        inputs.append("node_modules")
    digest = hashlib.sha256()
    files: list[tuple[Path, Path]] = []
    for name in inputs:
        source = runtime / name
        if not source.exists():
            raise PFBError("PFB_TOOLCHAIN_MISSING", "runtime-package-file")
        candidates = [source] if source.is_file() else sorted(source.rglob("*"))
        if source.is_symlink() or any(path.is_symlink() for path in candidates):
            raise PFBError("PFB_TOOLCHAIN_MISSING", "runtime-package-symlink")
        selected = [path for path in candidates if path.is_file()]
        for path in selected:
            if path.is_symlink():
                raise PFBError("PFB_TOOLCHAIN_MISSING", "runtime-package-symlink")
            relative = path.relative_to(runtime)
            digest.update(str(relative).encode())
            digest.update(path.read_bytes())
            files.append((path, relative))
    revision = digest.hexdigest()
    versions = workspace / "runtime-tools"
    versions.mkdir(mode=0o700, exist_ok=True)
    destination = versions / revision
    if not destination.exists():
        staging = Path(tempfile.mkdtemp(prefix=".prepare-", dir=versions))
        try:
            for source, relative in files:
                target = staging / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(source, target)
            if not self_contained:
                (staging / "node_modules").symlink_to("../../runtime-node", target_is_directory=True)
            for name in ["scripts/runtime-cli.mjs", "dist/runtime/index.js"]:
                if not (staging / name).is_file():
                    raise PFBError("PFB_TOOLCHAIN_MISSING", "runtime-tool-entry")
            staging.rename(destination)
        finally:
            if staging.exists():
                shutil.rmtree(staging)
    link = workspace / "runtime-tool.next"
    link.unlink(missing_ok=True)
    link.symlink_to(Path("runtime-tools") / revision, target_is_directory=True)
    os.replace(link, workspace / "runtime-tool")
    return revision
