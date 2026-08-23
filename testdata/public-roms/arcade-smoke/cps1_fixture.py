"""Project-owned FBA2012 CPS1 ``1941`` fixture."""

from __future__ import annotations

from typing import Any

from cps_fixture_common import build_dat, materialize_entries
from cps_program import build_68000_program, build_z80_silence, split_cps1_byteswapped
from deterministic_zip import deterministic_zip


OUTPUT_ARCHIVE = "fbalpha2012_cps1/1941.zip"
OUTPUT_DAT = "fbalpha2012_cps1/fbalpha2012-cps1-smoke.dat"


def _graphics(size: int, lane: int) -> bytes:
    block = bytes(((index + lane * 3) & 0xFF for index in range(256)))
    return (block * (size // len(block) + 1))[:size]


def build_outputs(driver: dict[str, Any]) -> dict[str, bytes]:
    program = build_68000_program(0x40000, "cps1")
    odd, even = split_cps1_byteswapped(program)
    sources = {
        "41em_30.11f": odd,
        "41em_35.11h": even,
        "41em_31.12f": bytes(0x20000),
        "41em_36.12h": bytes(0x20000),
        "41-32m.8h": bytes(0x80000),
        "41-5m.7a": _graphics(0x80000, 0),
        "41-7m.9a": _graphics(0x80000, 1),
        "41-1m.3a": _graphics(0x80000, 2),
        "41-3m.5a": _graphics(0x80000, 3),
        "41_9.12b": build_z80_silence(0x10000),
        "41_18.11c": bytes(0x20000),
        "41_19.12c": bytes(0x20000),
    }
    for specification in driver["entries"]:
        if specification["region"] == "optional":
            marker = f"RETROM PUBLIC OPTIONAL {specification['name']}".encode()
            sources[specification["name"]] = marker.ljust(specification["size"], b"\0")
    entries = materialize_entries(driver, sources)
    return {
        OUTPUT_ARCHIVE: deterministic_zip(entries),
        OUTPUT_DAT: build_dat("1941", "Retrom public FBA2012 CPS1 smoke", entries),
    }
