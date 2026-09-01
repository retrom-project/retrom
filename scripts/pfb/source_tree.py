"""Canonical Git worktree and migration fingerprints."""

from __future__ import annotations

import os
import stat
import subprocess
from pathlib import Path
from typing import Any

from .common import canonical_bytes, sha256_bytes, sha256_file
from .errors import PFBError


def git_output(root: Path, arguments: list[str], code: str) -> bytes:
    result = subprocess.run(
        ["git", "-C", str(root), *arguments], capture_output=True, check=False,
    )
    if result.returncode != 0:
        raise PFBError(code)
    return result.stdout


def git_text(root: Path, arguments: list[str], code: str) -> str:
    try:
        return git_output(root, arguments, code).decode("utf-8").strip()
    except UnicodeDecodeError as exc:
        raise PFBError(code) from exc


def worktree_identity(root: Path) -> dict[str, Any]:
    canonical = checked_worktree(root)
    branch = git_text(
        canonical, ["symbolic-ref", "--quiet", "--short", "HEAD"],
        "PFB_WORKTREE_DETACHED",
    )
    commit = git_text(canonical, ["rev-parse", "HEAD"], "PFB_WORKTREE_INVALID")
    if len(commit) != 40 or any(character not in "0123456789abcdef" for character in commit):
        raise PFBError("PFB_WORKTREE_INVALID")
    return {
        "root": str(canonical),
        "branch": branch,
        "commit": commit,
        "dirty": bool(git_output(canonical, ["status", "--porcelain=v1", "-z"], "PFB_WORKTREE_INVALID")),
        "sourceTreeSha256": source_tree_sha256(canonical),
    }


def checked_worktree(root: Path) -> Path:
    if not root.is_absolute():
        raise PFBError("PFB_WORKTREE_INVALID")
    try:
        info = root.lstat()
        canonical = root.resolve(strict=True)
    except OSError as exc:
        raise PFBError("PFB_WORKTREE_INVALID") from exc
    home = Path.home().resolve()
    if stat.S_ISLNK(info.st_mode) or not canonical.is_dir() or canonical in {Path("/"), home}:
        raise PFBError("PFB_WORKTREE_INVALID")
    reported = Path(git_text(canonical, ["rev-parse", "--show-toplevel"], "PFB_WORKTREE_INVALID"))
    if reported.resolve(strict=True) != canonical:
        raise PFBError("PFB_WORKTREE_INVALID")
    return canonical


def source_tree_sha256(root: Path) -> str:
    paths = _git_paths(root)
    tracked = _tracked_entries(root)
    entries = []
    for relative in paths:
        target = root / relative
        try:
            info = target.lstat()
        except FileNotFoundError:
            continue
        tracked_mode, tracked_object = tracked.get(relative, (None, None))
        if tracked_mode == "160000" and stat.S_ISDIR(info.st_mode):
            nested = {"indexCommit": tracked_object, "worktreeCommit": None, "sourceTreeSha256": None}
            if _is_worktree_root(target):
                nested["worktreeCommit"] = git_text(
                    target, ["rev-parse", "HEAD"], "PFB_WORKTREE_INVALID",
                )
                nested["sourceTreeSha256"] = source_tree_sha256(target)
            contents = canonical_bytes(nested)
            mode = "160000"
        elif stat.S_ISLNK(info.st_mode):
            contents = os.readlink(target).encode("utf-8")
            mode = "120000"
        elif stat.S_ISREG(info.st_mode):
            contents = None
            mode = tracked_mode or ("100755" if info.st_mode & stat.S_IXUSR else "100644")
        else:
            raise PFBError("PFB_WORKTREE_INVALID", relative)
        entries.append({
            "path": relative,
            "mode": mode,
            "sha256": sha256_bytes(contents) if contents is not None else sha256_file(target),
        })
    return sha256_bytes(canonical_bytes(entries))


def _is_worktree_root(root: Path) -> bool:
    result = subprocess.run(
        ["git", "-C", str(root), "rev-parse", "--show-toplevel"],
        capture_output=True,
        check=False,
        text=True,
    )
    if result.returncode != 0:
        return False
    try:
        return Path(result.stdout.strip()).resolve(strict=True) == root.resolve(strict=True)
    except OSError:
        return False


def migration_tree_sha256(root: Path) -> str:
    entries = []
    migration_root = root / "migrations"
    for target in sorted(migration_root.rglob("*")):
        if not target.is_file() or target.is_symlink():
            continue
        relative = target.relative_to(root).as_posix()
        mode = "100755" if target.stat().st_mode & stat.S_IXUSR else "100644"
        entries.append({"path": relative, "mode": mode, "sha256": sha256_file(target)})
    return sha256_bytes(canonical_bytes(entries))


def _git_paths(root: Path) -> list[str]:
    raw = git_output(
        root, ["ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        "PFB_WORKTREE_INVALID",
    )
    result = []
    for item in raw.split(b"\0"):
        if not item:
            continue
        try:
            path = item.decode("utf-8")
        except UnicodeDecodeError as exc:
            raise PFBError("PFB_WORKTREE_INVALID") from exc
        candidate = Path(path)
        if candidate.is_absolute() or ".." in candidate.parts or candidate.as_posix() != path:
            raise PFBError("PFB_WORKTREE_INVALID", path)
        result.append(path)
    return sorted(set(result), key=lambda value: value.encode("utf-8"))


def _tracked_entries(root: Path) -> dict[str, tuple[str, str]]:
    raw = git_output(root, ["ls-files", "--stage", "-z"], "PFB_WORKTREE_INVALID")
    result: dict[str, tuple[str, str]] = {}
    for item in raw.split(b"\0"):
        if not item:
            continue
        prefix, separator, path_bytes = item.partition(b"\t")
        if not separator:
            raise PFBError("PFB_WORKTREE_INVALID")
        parts = prefix.split(b" ")
        if len(parts) < 2:
            raise PFBError("PFB_WORKTREE_INVALID")
        mode = parts[0].decode("ascii")
        object_id = parts[1].decode("ascii")
        path = path_bytes.decode("utf-8")
        if mode not in {"100644", "100755", "120000", "160000"}:
            raise PFBError("PFB_WORKTREE_INVALID", path)
        result[path] = (mode, object_id)
    return result
