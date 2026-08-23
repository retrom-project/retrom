"""Project-owned FBA2012 CPS2 ``spf2xjd`` Phoenix fixture."""

from __future__ import annotations

from typing import Any

from cps_fixture_common import build_dat, materialize_entries
from cps_program import build_68000_program, build_z80_silence, byteswap_68000_words
from deterministic_zip import deterministic_zip


OUTPUT_ARCHIVE = "fbalpha2012_cps2/spf2xjd.zip"
OUTPUT_PARENT_ARCHIVE = "fbalpha2012_cps2/spf2t.zip"
OUTPUT_DAT = "fbalpha2012_cps2/fbalpha2012-cps2-smoke.dat"
PARENT_ENTRIES = {
    "retrom-parent.marker": b"RETROM PUBLIC CPS2 PARENT ARCHIVE - REQUIRED BY THE PINNED CORE LOADER\n",
}


def _graphics(size: int, lane: int) -> bytes:
    block = bytes((((index >> 4) + lane * 5) & 0xFF for index in range(256)))
    return (block * (size // len(block) + 1))[:size]


def build_outputs(driver: dict[str, Any]) -> dict[str, bytes]:
    sources = {
        "pzfjd.03a": byteswap_68000_words(build_68000_program(0x80000, "cps2")),
        "pzf.04": bytes(0x80000),
        "pzf.14m": _graphics(0x100000, 0),
        "pzf.16m": _graphics(0x100000, 1),
        "pzf.18m": _graphics(0x100000, 2),
        "pzf.20m": _graphics(0x100000, 3),
        "pzf.01": build_z80_silence(0x20000),
        "pzf.02": bytes(0x20000),
        "pzf.11m": bytes(0x200000),
        "pzf.12m": bytes(0x200000),
    }
    entries = materialize_entries(driver, sources)
    return {
        OUTPUT_ARCHIVE: deterministic_zip(entries),
        OUTPUT_PARENT_ARCHIVE: deterministic_zip(PARENT_ENTRIES),
        OUTPUT_DAT: build_dat(
            "spf2xjd",
            "Retrom public FBA2012 CPS2 smoke",
            entries,
            parent=("spf2t", "Retrom public FBA2012 CPS2 loader parent", PARENT_ENTRIES),
        ),
    }
