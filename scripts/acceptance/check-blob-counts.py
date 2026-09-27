#!/usr/bin/env python3
"""Read-only audit of explicit counts against actual protective rows and archives."""
from collections import Counter, defaultdict, deque
import json
from pathlib import Path
import re
import sqlite3
import sys


def check(path):
    registry = Path(__file__).resolve().parents[2] / "internal/persistence/blobregistry/registry.json"
    edges = json.loads(registry.read_text())["edges"]
    with sqlite3.connect(path.resolve().as_uri() + "?mode=ro", uri=True) as database:
        database.execute("BEGIN")
        actual = dict(database.execute("SELECT id,ref_count FROM blobs"))
        expected = Counter()
        for edge in edges:
            if edge["class"] != "PROTECTIVE":
                continue
            table, column = edge["table"], edge["column"]
            if not all(re.fullmatch(r"[a-z_]+", name) for name in (table, column)):
                raise ValueError("invalid registry identifier")
            for blob, count in database.execute(
                f'SELECT "{column}",count(*) FROM "{table}" WHERE "{column}" IS NOT NULL GROUP BY "{column}"',
            ):
                expected[blob] += count
        members = defaultdict(Counter)
        for archive, member, count in database.execute(
            "SELECT archive_blob_id,materialized_blob_id,count(*) FROM archive_entries "
            "WHERE materialized_blob_id IS NOT NULL AND materialized_blob_id<>archive_blob_id "
            "GROUP BY archive_blob_id,materialized_blob_id",
        ):
            members[archive][member] += count
        pending = deque(expected)
        visited = set()
        while pending:
            parent = pending.popleft()
            if parent in visited:
                continue
            visited.add(parent)
            for member, count in members[parent].items():
                expected[member] += count
                pending.append(member)
        mismatches = sum(actual.get(blob) != expected[blob] for blob in actual.keys() | expected.keys())
        if mismatches:
            raise ValueError(f"Blob reference count mismatch: {mismatches} objects")
        print(f"blob_reference_counts=verified blobs={len(actual)} protective_edges={sum(expected.values())}")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("usage: check-blob-counts.py DATABASE")
    check(Path(sys.argv[1]))
