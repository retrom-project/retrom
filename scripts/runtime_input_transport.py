"""Release URLs transport Provider chunks; the checked-in manifest owns every digest."""
from __future__ import annotations

from typing import Any
from urllib.parse import urlsplit


def transport_base(manifest: dict[str, Any], configured: str) -> str:
    if configured:
        value = urlsplit(configured)
        # Plain HTTP is accepted only for a loopback mirror (local candidates and tests).
        loopback = value.scheme == "http" and value.hostname in {"127.0.0.1", "localhost", "::1"}
        if (value.scheme != "https" and not loopback) or not value.netloc or value.username or value.password or \
                value.query or value.fragment:
            raise ValueError("RUNTIME_INPUT_TRANSPORT_INVALID")
        return configured.rstrip("/")
    release = manifest["release"]
    if release is not None:
        return f'{manifest["repository"]}/releases/download/{release["tag"]}'
    return ""
