#!/usr/bin/env python3
"""Generate the byte-stable EasyRPG RTP layout registry from pinned Player CSV files."""

from __future__ import annotations

import argparse
import csv
import io
import json
import tarfile


PLAYER_COMMIT = "78328fa29f465315291e161130e6682f69410370"
ROOT = f"Player-{PLAYER_COMMIT}/resources/rtp_table"


def registry(archive: tarfile.TarFile, filename: str) -> dict[str, object]:
    source = archive.extractfile(f"{ROOT}/{filename}")
    if source is None:
        raise SystemExit(f"missing {filename}")
    rows = csv.reader(io.TextIOWrapper(source, encoding="utf-8", newline=""))
    next(rows)
    category = ""
    categories: set[str] = set()
    resources: set[str] = set()
    for row in rows:
        if row and row[0]:
            category = row[0]
            categories.add(category)
        for alias in row[1:]:
            if category and alias:
                resources.add(f"{category}/{alias}")
    return {
        "categories": sorted(categories),
        "registeredResources": sorted(resources),
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("source_tar")
    parser.add_argument("output")
    args = parser.parse_args()
    with tarfile.open(args.source_tar, "r:gz") as archive:
        generations = {
            "RPG2000": registry(archive, "RTP2k.csv"),
            "RPG2003": registry(archive, "RTP2k3.csv"),
        }
    result = {
        "schemaVersion": 1,
        "sourcePlayerCommit": PLAYER_COMMIT,
        "extensions": {
            # Player src/filefinder.h at PLAYER_COMMIT is the fixed decoder
            # lookup contract. Movie extensions come from filefinder.cpp's
            # SUPPORT_MOVIES branch.
            "image": [".bmp", ".png", ".xyz"],
            "movie": [".avi", ".mpg"],
            "music": [".mid", ".midi", ".mp3", ".oga", ".ogg", ".opus", ".wav", ".wma"],
            "sound": [".mp3", ".oga", ".ogg", ".opus", ".wav", ".wma"],
        },
        "generations": generations,
    }
    with open(args.output, "w", encoding="utf-8", newline="\n") as output:
        json.dump(result, output, ensure_ascii=False, indent=2, sort_keys=True)
        output.write("\n")


if __name__ == "__main__":
    main()
