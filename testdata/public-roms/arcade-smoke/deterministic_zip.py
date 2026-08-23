"""Canonical ZIP serialization for public arcade fixtures."""

from __future__ import annotations

import io
import zipfile


def deterministic_zip(entries: dict[str, bytes], *, archive_comment: bytes = b"") -> bytes:
    output = io.BytesIO()
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_STORED) as archive:
        # Callers construct entries in their locked driver order. Preserve that
        # order so the pre-existing MAME/FBNeo fixture bytes remain unchanged.
        for name in entries:
            info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_STORED
            info.create_system = 3
            info.external_attr = 0o100644 << 16
            archive.writestr(info, entries[name])
        archive.comment = archive_comment
    return output.getvalue()
