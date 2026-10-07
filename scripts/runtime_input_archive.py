"""Read one npm tool archive without links, path escapes or unbounded extraction."""
from __future__ import annotations

import hashlib
from pathlib import Path, PurePosixPath
import shutil
import tarfile

MAX_UNPACKED = 24 * 1024**3


def _members(source):
    total, top, seen = 0, None, set()
    for index, member in enumerate(source):
        path = PurePosixPath(member.name)
        if index >= 200_000 or path.is_absolute() or ".." in path.parts or not path.parts or "\\" in member.name:
            raise ValueError("IMAGE_ARCHIVE_PATH_INVALID")
        if top is None:
            top = path.parts[0]
        if path.parts[0] != top or not (member.isdir() or member.isfile()):
            raise ValueError("IMAGE_ARCHIVE_TYPE_INVALID")
        if len(path.parts) == 1:
            if not member.isdir():
                raise ValueError("IMAGE_ARCHIVE_ROOT_INVALID")
            continue
        relative = PurePosixPath(*path.parts[1:]).as_posix()
        if relative in seen or len(relative) > 4096 or len(path.parts) > 64:
            raise ValueError("IMAGE_ARCHIVE_DUPLICATE_OR_LIMIT")
        seen.add(relative)
        total += member.size
        if member.size < 0 or total > MAX_UNPACKED:
            raise ValueError("IMAGE_ARCHIVE_SIZE_LIMIT")
        yield member, relative
    if not seen:
        raise ValueError("IMAGE_ARCHIVE_EMPTY")


def archive_integrity(archive: Path) -> dict:
    files = {}
    with tarfile.open(archive, "r:gz") as source:
        for member, relative in _members(source):
            if not member.isfile():
                continue
            stream = source.extractfile(member)
            if stream is None:
                raise ValueError("IMAGE_ARCHIVE_FILE_INVALID")
            digest = hashlib.sha256()
            with stream:
                for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                    digest.update(chunk)
            files[relative] = {"sizeBytes": member.size, "sha256": digest.hexdigest(), "executable": bool(member.mode & 0o111)}
    return files


def unpack_tool(archive: Path, target: Path) -> None:
    with tarfile.open(archive, "r:gz") as source:
        for member, relative in _members(source):
            output = target / relative
            if member.isdir():
                output.mkdir(parents=True, exist_ok=True)
                continue
            output.parent.mkdir(parents=True, exist_ok=True)
            stream = source.extractfile(member)
            if stream is None:
                raise ValueError("IMAGE_ARCHIVE_FILE_INVALID")
            with stream, output.open("xb") as destination:
                shutil.copyfileobj(stream, destination, 1024 * 1024)
            output.chmod(0o755 if member.mode & 0o111 else 0o644)
