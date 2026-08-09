#!/usr/bin/env python3
"""Materialize ignored DOS smoke archives with a deterministic direct launcher."""

from __future__ import annotations

import json
import zipfile
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
MANIFEST_PATH = Path(__file__).with_name("fixtures.json")


def launcher(entry: str) -> bytes:
    dos_entry = entry.replace("/", "\\")
    return f"C:\\{dos_entry}".encode("ascii")


def main() -> int:
    manifest = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))
    for fixture in manifest["fixtures"]:
        entry = fixture.get("dosEntry")
        output_name = fixture.get("materializedGamePath")
        if not entry or not output_name:
            continue
        source = REPOSITORY_ROOT / fixture["game"]["localPath"]
        output = REPOSITORY_ROOT / output_name
        output.parent.mkdir(parents=True, exist_ok=True)
        temporary = output.with_suffix(".tmp")
        with zipfile.ZipFile(source) as archive, zipfile.ZipFile(
            temporary, "w", allowZip64=False
        ) as destination:
            launch_info = zipfile.ZipInfo("AUTOBOOT.DBP", (1980, 1, 1, 0, 0, 0))
            launch_info.compress_type = zipfile.ZIP_STORED
            launch_info.external_attr = 0o100644 << 16
            destination.writestr(launch_info, launcher(entry))
            for source_info in archive.infolist():
                if source_info.filename.lower() in {"dosbox.bat", "autoboot.dbp"}:
                    continue
                destination.writestr(source_info, archive.read(source_info))
        temporary.replace(output)
        print(f"materialized {output.relative_to(REPOSITORY_ROOT)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
