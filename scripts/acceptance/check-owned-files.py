#!/usr/bin/env python3
"""Read-only acceptance audit of file identities and published owner boundaries."""
from contextlib import closing
from pathlib import Path
import sqlite3
import sys
import uuid


def check(path):
    with closing(sqlite3.connect(path.resolve().as_uri() + "?mode=ro", uri=True)) as db:
        db.execute("BEGIN")
        if db.execute("PRAGMA foreign_key_check").fetchone():
            raise ValueError("file audit found broken foreign keys")
        rows = db.execute("SELECT id,size_bytes,owner_kind,owner_id,retired_at_ms FROM stored_files").fetchall()
        inodes = set()
        for file_id, size, kind, owner, retired in rows:
            if str(uuid.UUID(file_id)) != file_id or (kind != "STAGING" and not owner):
                raise ValueError("invalid file identity or owner")
            target = path.parent / "files" / file_id[:2] / file_id
            if retired is not None:
                continue  # Deletion executes outside the database transaction.
            stat = target.stat()
            inode = (stat.st_dev, stat.st_ino)
            if stat.st_size != size or inode in inodes:
                raise ValueError("file is incomplete or shares its physical inode")
            inodes.add(inode)
        for table in ("game_files", "game_assets"):
            invalid = db.execute(f"SELECT count(*) FROM {table} link JOIN games game ON game.id=link.game_id "
                                 "JOIN stored_files file ON file.id=link.blob_id WHERE game.status='PUBLISHED' "
                                 "AND (file.owner_kind<>'GAME' OR file.owner_id<>game.id OR file.retired_at_ms IS NOT NULL)").fetchone()[0]
            if invalid:
                raise ValueError(f"{table} crosses game ownership: {invalid}")
        invalid = db.execute("SELECT count(*) FROM variant_files link JOIN game_variants variant ON variant.id=link.game_variant_id "
                             "JOIN games game ON game.id=variant.game_id JOIN stored_files file ON file.id=link.blob_id "
                             "WHERE game.status='PUBLISHED' AND link.role<>'BIOS_BUNDLE' "
                             "AND (file.owner_kind<>'GAME' OR file.owner_id<>game.id OR file.retired_at_ms IS NOT NULL)").fetchone()[0]
        if invalid:
            raise ValueError(f"variant files cross game ownership: {invalid}")
        print(f"owned_files=verified registered={len(rows)} retained_physical_files={len(inodes)}")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("usage: check-owned-files.py DATABASE")
    check(Path(sys.argv[1]))
