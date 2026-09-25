#!/usr/bin/env python3
"""Build a deterministic, self-authored Lutro cartridge."""
from pathlib import Path
from zipfile import ZipFile, ZipInfo, ZIP_DEFLATED

root = Path(__file__).resolve().parent
contents = (root / "main.lua").read_bytes()
info = ZipInfo("main.lua", (1980, 1, 1, 0, 0, 0))
info.compress_type = ZIP_DEFLATED
info.external_attr = 0o100644 << 16
with ZipFile(root / "lutro-smoke.lutro", "w") as archive:
    archive.writestr(info, contents)
