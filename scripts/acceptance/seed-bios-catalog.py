#!/usr/bin/env python3
"""Expand the local acceptance BIOS catalog to a deterministic target size."""

from __future__ import annotations

import hashlib
import sqlite3
import sys
from pathlib import Path


def main() -> int:
    if len(sys.argv) != 3:
        raise SystemExit("usage: seed-bios-catalog.py DATABASE TARGET_COUNT")
    database_path = Path(sys.argv[1]).resolve()
    target_count = int(sys.argv[2])
    connection = sqlite3.connect(database_path)
    connection.row_factory = sqlite3.Row
    try:
        current = connection.execute(
            """SELECT count(*) FROM bios_requirements requirement
               JOIN core_artifacts artifact ON artifact.id=requirement.core_artifact_id
               WHERE requirement.enabled=1 AND artifact.enabled=1"""
        ).fetchone()[0]
        if current > target_count:
            raise RuntimeError(f"enabled BIOS catalog already exceeds target: {current} > {target_count}")
        template = connection.execute(
            """SELECT requirement.* FROM bios_requirements requirement
               JOIN core_artifacts artifact ON artifact.id=requirement.core_artifact_id
               WHERE requirement.enabled=1 AND artifact.enabled=1 AND requirement.source_kind='STATIC'
               ORDER BY requirement.id LIMIT 1"""
        ).fetchone()
        if template is None:
            raise RuntimeError("no enabled STATIC BIOS requirement is available as a seed template")
        columns = [row[1] for row in connection.execute("PRAGMA table_info(bios_requirements)")]
        placeholders = ",".join("?" for _ in columns)
        insert = f"INSERT INTO bios_requirements({','.join(columns)}) VALUES({placeholders})"
        for index in range(target_count - current):
            logical_name = f"acceptance_catalog_{index:03d}.bin"
            expected_bytes = f"deterministic BIOS catalog fixture {index:03d}".encode()
            values = dict(template)
            values.update(
                id=f"acceptance-bios-{index:03d}",
                source_kind="STATIC",
                dat_machine_name=None,
                logical_name=logical_name,
                requirement_mode="OPTIONAL",
                condition_code=None,
                activation_options_json=None,
                catalog_digest=hashlib.sha256(logical_name.encode()).hexdigest(),
                size_bytes=len(expected_bytes),
                md5=None,
                sha1=None,
                sha256=hashlib.sha256(expected_bytes).hexdigest(),
                source_url=f"https://example.invalid/{logical_name}",
                source_version="acceptance-v1",
                enabled=1,
                version=1,
                created_at_ms=1,
                updated_at_ms=1,
            )
            connection.execute(insert, [values[column] for column in columns])
        connection.commit()
        final = connection.execute(
            """SELECT count(*) FROM bios_requirements requirement
               JOIN core_artifacts artifact ON artifact.id=requirement.core_artifact_id
               WHERE requirement.enabled=1 AND artifact.enabled=1"""
        ).fetchone()[0]
        if final != target_count:
            raise RuntimeError(f"seeded BIOS catalog has {final} entries, expected {target_count}")
        print(f"bios_catalog_count={final}")
    finally:
        connection.close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
