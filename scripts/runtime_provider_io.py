from __future__ import annotations

import json
import os
import tempfile
import urllib.request
from pathlib import Path
from typing import Any


def _load_json(path: Path, code: str) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise ValueError(code) from error


def _fetch_bytes(url: str, maximum: int) -> bytes:
    request = urllib.request.Request(url, headers={"User-Agent": "retrom-runtime-provider"})
    try:
        with urllib.request.urlopen(request, timeout=60) as response:  # noqa: S310 -- HTTPS validated above.
            final_url = response.geturl()
            if not isinstance(final_url, str) or not final_url.startswith("https://"):
                raise ValueError("PROVIDER_DOWNLOAD_INVALID")
            contents = response.read(maximum + 1)
    except (OSError, ValueError) as error:
        raise ValueError("PROVIDER_DOWNLOAD_INVALID") from error
    if not contents or len(contents) > maximum:
        raise ValueError("PROVIDER_DOWNLOAD_INVALID")
    return contents


def _write_json_atomic(path: Path, value: Any) -> None:
    _write_bytes_atomic(path, (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode("utf-8"))


def _write_bytes_atomic(path: Path, contents: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    temporary = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "wb") as output:
            output.write(contents)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
    finally:
        if temporary.exists():
            temporary.unlink()
