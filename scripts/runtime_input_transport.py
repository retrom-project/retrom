"""Artifact URLs transport bytes; the checked-in manifest owns every digest."""
from __future__ import annotations

import hashlib
from pathlib import Path
from typing import Any, Callable
from urllib.parse import urlsplit

try:
    from .runtime_provider_cache import cached_download
    from .runtime_provider_io import _fetch_bytes
except ImportError:
    from runtime_provider_cache import cached_download
    from runtime_provider_io import _fetch_bytes


def transport_base(manifest: dict[str, Any], configured: str) -> str:
    if configured:
        value = urlsplit(configured)
        if value.scheme != "https" or not value.netloc or value.username or value.password or value.query or value.fragment:
            raise ValueError("RUNTIME_INPUT_TRANSPORT_INVALID")
        return configured.rstrip("/")
    release = manifest["release"]
    if release is not None:
        return f'{manifest["repository"]}/releases/download/{release["tag"]}'
    return ""


def artifact(record: dict[str, Any], cache: Path, archive_root: Path | None, base_url: str,
             fetch: Callable[[str, int], bytes] = _fetch_bytes) -> Path:
    destination = cache / "runtime-inputs" / record["sha256"] / record["archive"]

    def validate(contents: bytes) -> None:
        if len(contents) != record["sizeBytes"] or hashlib.sha256(contents).hexdigest() != record["sha256"]:
            raise ValueError("RUNTIME_INPUT_ARTIFACT_DIGEST_INVALID")

    def download() -> bytes:
        if archive_root is not None:
            source = archive_root / record["archive"]
            if source.is_symlink() or not source.is_file() or source.stat().st_size != record["sizeBytes"]:
                raise ValueError("RUNTIME_INPUT_ARCHIVE_REQUIRED:" + record["archive"])
            return source.read_bytes()
        if not base_url:
            raise ValueError("RUNTIME_INPUT_TRANSPORT_REQUIRED: provide an archive directory or HTTPS base for the pinned unpublished artifacts")
        return fetch(base_url + "/" + record["archive"], record["sizeBytes"])

    cached_download(destination, download, validate, record["sizeBytes"])
    return destination
