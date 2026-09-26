#!/usr/bin/env python3
"""Build a deterministic, self-authored Lutro cartridge."""
from argparse import ArgumentParser
from pathlib import Path
from zipfile import ZipFile, ZipInfo, ZIP_DEFLATED

root = Path(__file__).resolve().parent
parser = ArgumentParser()
parser.add_argument("--output", type=Path)
parser.add_argument("--marker")
args = parser.parse_args()
if args.marker and (args.output is None or "\n" in args.marker or "\r" in args.marker):
    parser.error("--marker requires --output and must be one line")
contents = (root / "main.lua").read_bytes()
if args.marker:
    contents += f"\n-- {args.marker}\n".encode("utf-8")
info = ZipInfo("main.lua", (1980, 1, 1, 0, 0, 0))
info.compress_type = ZIP_DEFLATED
info.external_attr = 0o100644 << 16
output = args.output or root / "lutro-smoke.lutro"
output.parent.mkdir(parents=True, exist_ok=True)
with ZipFile(output, "w") as archive:
    archive.writestr(info, contents)
