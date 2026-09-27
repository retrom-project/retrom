#!/usr/bin/env python3
"""Read-only audit of published game directories and independent physical bytes."""
from contextlib import closing
from pathlib import Path, PurePosixPath
import hashlib
import json
import sqlite3
import sys
import uuid


def check(path):
    root = path.parent.resolve()
    with closing(sqlite3.connect(path.resolve().as_uri() + "?mode=ro", uri=True)) as db:
        db.execute("BEGIN")
        if db.execute("PRAGMA foreign_key_check").fetchone():
            raise ValueError("file audit found broken foreign keys")
        rows = []
        for table in ("game_files", "game_assets"):
            rows.extend(db.execute(f"SELECT game.id,link.file_record FROM {table} link JOIN games game "
                                   "ON game.id=link.game_id WHERE game.status='PUBLISHED'").fetchall())
        rows.extend(db.execute("SELECT game.id,link.file_record FROM variant_files link "
                               "JOIN game_variants variant ON variant.id=link.game_variant_id "
                               "JOIN games game ON game.id=variant.game_id "
                               "WHERE game.status='PUBLISHED' AND link.role<>'BIOS_BUNDLE'").fetchall())
        inodes, checked = {}, set()
        for owner, encoded in rows:
            owner = str(uuid.UUID(owner))
            record = json.loads(encoded)
            relative = PurePosixPath(record["path"])
            prefix = PurePosixPath(f"files/{owner[-2:]}/{owner}")
            if relative.is_absolute() or ".." in relative.parts or not relative.is_relative_to(prefix):
                raise ValueError("file crosses game ownership")
            target = root / relative
            if not target.resolve().is_relative_to(root / prefix):
                raise ValueError("file crosses game ownership through symlink")
            if (owner, encoded) in checked:
                continue
            stat = target.stat()
            inode = (stat.st_dev, stat.st_ino)
            if stat.st_size != record["size_bytes"] or inode in inodes and inodes[inode] != owner:
                raise ValueError("file is incomplete or shares its physical inode")
            with target.open("rb") as source:
                if hashlib.file_digest(source, "sha256").hexdigest() != record["sha256"]:
                    raise ValueError("file content differs from domain record")
            inodes[inode] = owner
            checked.add((owner, encoded))
        print(f"game_directories=verified domain_files={len(checked)} physical_files={len(inodes)}")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("usage: check-owned-files.py DATABASE")
    check(Path(sys.argv[1]))
