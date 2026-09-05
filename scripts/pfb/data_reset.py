"""Recoverable, stopped-PFB data rebuild; never parses an obsolete database or base."""

from pathlib import Path
from typing import Any, Callable

from .docker import ensure_workspace, import_provider_base, workspace_paths
from .errors import PFBError


def reset_workspace_data(
    root: Path,
    archive_name: str,
    source_root: Path | None,
    validate: Callable[[Path, Path], dict[str, Any]],
) -> dict[str, Any]:
    paths = workspace_paths(root)
    selected = ["data"]
    if source_root is not None:
        _validate_source(root, source_root, validate)
        selected.extend(["providerActive", "providerDev"])
    for name in ["root", "providers", *selected]:
        path = paths[name]
        if path.is_symlink() or not path.resolve().is_relative_to(root.resolve()):
            raise PFBError("PFB_DATA_RESET_INVALID", "workspace-path")
    if not archive_name or Path(archive_name).name != archive_name or archive_name in {".", ".."}:
        raise PFBError("PFB_DATA_RESET_INVALID", "archive-name")
    backup = paths["root"] / "reset-backups" / archive_name
    if backup.exists() or backup.parent.is_symlink():
        raise PFBError("PFB_DATA_RESET_INVALID", "archive-path")
    ensure_workspace(root)
    backup.mkdir(parents=True, mode=0o700)
    moved: list[tuple[Path, Path]] = []
    try:
        for name in selected:
            original = paths[name]
            if original.exists():
                archived = backup / original.relative_to(paths["root"])
                archived.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                original.rename(archived)
                moved.append((original, archived))
        paths["data"].mkdir(mode=0o700)
        result: dict[str, Any] = {"backup": str(backup)}
        if source_root is not None:
            result.update(import_provider_base(root, source_root, validate, _reject_existing_base))
        return result
    except Exception:
        for original, archived in reversed(moved):
            if original.is_file():
                original.unlink()
            elif original.is_dir():
                original.rmdir()  # Only empty replacement directories may be removed.
            archived.rename(original)
        raise


def _validate_source(root: Path, source: Path, validate: Callable) -> None:
    try:
        resolved = source.resolve(strict=True)
        if not resolved.is_dir() or resolved.is_relative_to((root / ".pfb").resolve()):
            raise PFBError("PFB_DATA_RESET_INVALID", "source-root")
        validate(resolved / "active.json", resolved / "installed")
    except (OSError, TypeError, ValueError) as exc:
        raise PFBError("PFB_PROVIDER_BASE_INVALID", "source-validation") from exc


def _reject_existing_base(_current: dict[str, Any], _incoming: dict[str, Any]) -> None:
    raise PFBError("PFB_DATA_RESET_INVALID", "base-reappeared-during-reset")
