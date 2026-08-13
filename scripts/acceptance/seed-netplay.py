#!/usr/bin/env python3
"""Seed authorized netplay fixtures into an isolated acceptance database."""

from __future__ import annotations

import hashlib
import json
import os
import shutil
import sqlite3
import sys
import uuid
import zlib
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
NOW_MS = 1_786_000_000_000

USERS = (
    ("01980000-0000-7000-8000-00000000a002", "01980000-0000-7000-8000-00000000b002", "alice", "Alice"),
    ("01980000-0000-7000-8000-00000000a003", "01980000-0000-7000-8000-00000000b003", "charlie", "Charlie"),
)

GAMES = (
    {
        "game_id": "01980000-0000-7000-8000-00000000c101",
        "metadata_id": "01980000-0000-7000-8000-00000000c102",
        "content_id": "01980000-0000-7000-8000-00000000c103",
        "variant_id": "01980000-0000-7000-8000-00000000c104",
        "revision_id": "01980000-0000-7000-8000-00000000c105",
        "blob_id": "01980000-0000-7000-8000-00000000c106",
        "title": "F-1 Race",
        "platform_instance_id": "01980000-0000-7000-8000-000000000001",
        "core": "fceumm",
        "profile": "fceumm-423-v1",
        "logical_name": "f1-race.nes",
        "fixture": "data/example/local-fixtures/netplay/f1-race.nes",
        "sha256": "29208764886f14de20fe82b32ab034130915f6392103874d202fcbbfb8a02ee4",
        "size": 24592,
        "emulator_game_id": 9_101,
    },
    {
        "game_id": "01980000-0000-7000-8000-00000000c201",
        "metadata_id": "01980000-0000-7000-8000-00000000c202",
        "content_id": "01980000-0000-7000-8000-00000000c203",
        "variant_id": "01980000-0000-7000-8000-00000000c204",
        "revision_id": "01980000-0000-7000-8000-00000000c205",
        "blob_id": "01980000-0000-7000-8000-00000000c206",
        "title": "Lode Runner",
        "platform_instance_id": "01980000-0000-7000-8000-000000000006",
        "core": "fbneo",
        "profile": "fbneo-423-v1",
        "logical_name": "ldrun.zip",
        "fixture": "data/example/local-fixtures/netplay/ldrun.zip",
        "sha256": "b45507a74f739e27a5486d79901016b78e061c4db2025435d4df37702553e8d9",
        "size": 59720,
        "emulator_game_id": 9_102,
    },
)


def checksums(path: Path) -> tuple[str, str, str, str, int]:
    contents = path.read_bytes()
    return (
        hashlib.sha256(contents).hexdigest(),
        hashlib.md5(contents, usedforsecurity=False).hexdigest(),
        hashlib.sha1(contents, usedforsecurity=False).hexdigest(),
        f"{zlib.crc32(contents) & 0xFFFFFFFF:08x}",
        len(contents),
    )


def materialize_cas(data_dir: Path, source: Path, digest: str) -> None:
    target = data_dir / "blobs" / "sha256" / digest[:2] / digest[2:4] / digest
    target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    if target.exists():
        if checksums(target)[0] != digest:
            raise RuntimeError("NETPLAY_ACCEPTANCE_CAS_DRIFT")
        return
    temporary = target.with_name(f".{target.name}.{uuid.uuid4().hex}")
    shutil.copyfile(source, temporary)
    os.chmod(temporary, 0o600)
    os.replace(temporary, target)


def materialize_bytes(data_dir: Path, contents: bytes) -> tuple[str, str, str, str, int]:
    digest = hashlib.sha256(contents).hexdigest()
    target = data_dir / "blobs" / "sha256" / digest[:2] / digest[2:4] / digest
    target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    if not target.exists():
        temporary = target.with_name(f".{target.name}.{uuid.uuid4().hex}")
        temporary.write_bytes(contents)
        os.chmod(temporary, 0o600)
        os.replace(temporary, target)
    return (
        digest,
        hashlib.md5(contents, usedforsecurity=False).hexdigest(),
        hashlib.sha1(contents, usedforsecurity=False).hexdigest(),
        f"{zlib.crc32(contents) & 0xFFFFFFFF:08x}",
        len(contents),
    )


def seed_user(connection: sqlite3.Connection, user: tuple[str, str, str, str]) -> None:
    profile_id, user_id, username, display_name = user
    connection.execute(
        "INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,?,?)",
        (profile_id, display_name, NOW_MS),
    )
    connection.execute(
        """
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,'USER','ENABLED',?,?)
""",
        (user_id, profile_id, username, display_name, NOW_MS, NOW_MS),
    )
    connection.execute(
        """
INSERT INTO user_credentials(user_id,password_hash,password_scheme,password_changed_at_ms,created_at_ms)
SELECT ?,password_hash,password_scheme,?,? FROM user_credentials
WHERE user_id=(SELECT id FROM users WHERE username='test')
""",
        (user_id, NOW_MS, NOW_MS),
    )


def seed_game(connection: sqlite3.Connection, data_dir: Path, game: dict[str, object]) -> None:
    fixture = ROOT / str(game["fixture"])
    sha256_value, md5_value, sha1_value, crc32_value, size = checksums(fixture)
    if sha256_value != game["sha256"] or size != game["size"]:
        raise RuntimeError(f"NETPLAY_FIXTURE_DRIFT:{game['profile']}")
    materialize_cas(data_dir, fixture, sha256_value)
    artifact = connection.execute(
        """
SELECT id FROM core_artifacts
WHERE core_id=? AND emulatorjs_version='4.2.3' AND enabled=1
""",
        (game["core"],),
    ).fetchone()
    if artifact is None:
        raise RuntimeError(f"NETPLAY_CORE_UNAVAILABLE:{game['core']}")
    source_manifest = json.dumps(
        {"schemaVersion": 1, "fixture": game["profile"], "sha256": sha256_value},
        sort_keys=True,
        separators=(",", ":"),
    )
    source_digest = hashlib.sha256(source_manifest.encode()).hexdigest()
    validation_digest = hashlib.sha256(f"validation:{game['profile']}".encode()).hexdigest()
    connection.execute(
        """
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES(?,?,?,?,?,?,?,?)
""",
        (game["blob_id"], sha256_value, size, md5_value, sha1_value, crc32_value,
         "application/zip" if str(game["logical_name"]).endswith(".zip") else "application/octet-stream", NOW_MS),
    )
    connection.execute(
        """
INSERT INTO game_metadata_revisions(
  id,game_id,title,description,developer,publisher,genre,players,release_year,source_kind,created_at_ms
) VALUES(?,?,?,'Netplay acceptance fixture','','','',2,NULL,'ADMIN_EDIT',?)
""",
        (game["metadata_id"], game["game_id"], game["title"], NOW_MS),
    )
    connection.execute(
        """
INSERT INTO game_content_revisions(
  id,game_id,content_kind,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms
) VALUES(?,?,'SINGLE_FILE','ADMIN_REPLACE',?,?,?,?)
""",
        (game["content_id"], game["game_id"], game["profile"], source_manifest, source_digest, NOW_MS),
    )
    connection.execute(
        """
INSERT INTO games(
  id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,
  search_text,version,created_at_ms,updated_at_ms
) VALUES(?,?,'PUBLISHED',?,?,?,1,?,?)
""",
        (game["game_id"], game["platform_instance_id"], game["metadata_id"], game["content_id"],
         str(game["title"]).lower(), NOW_MS, NOW_MS),
    )
    connection.execute(
        """
INSERT INTO game_content_files(game_content_revision_id,role,logical_name,blob_id,sort_order)
VALUES(?,'CONTENT',?,?,0)
""",
        (game["content_id"], game["logical_name"], game["blob_id"]),
    )
    connection.execute(
        """
INSERT INTO game_variants(id,game_id,core_id,current_revision_id,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,NULL,1,?,?)
""",
        (game["variant_id"], game["game_id"], game["core"], NOW_MS, NOW_MS),
    )
    connection.execute(
        """
INSERT INTO game_variant_revisions(
  id,game_variant_id,game_content_revision_id,core_artifact_id,validation_input_digest,
  emulator_game_id,status,compatibility_code,dependency_snapshot_json,created_at_ms
) VALUES(?,?,?,?,?,?,'READY','READY','{"schemaVersion":1,"bios":[]}',?)
""",
        (game["revision_id"], game["variant_id"], game["content_id"], artifact[0],
         validation_digest, game["emulator_game_id"], NOW_MS),
    )
    connection.execute(
        "UPDATE game_variants SET current_revision_id=? WHERE id=?",
        (game["revision_id"], game["variant_id"]),
    )


def seed_unsupported_game(connection: sqlite3.Connection) -> None:
    game_id = "01980000-0000-7000-8000-00000000c301"
    metadata_id = "01980000-0000-7000-8000-00000000c302"
    content_id = "01980000-0000-7000-8000-00000000c303"
    source_manifest = '{"schemaVersion":1,"fixture":"unsupported"}'
    source_digest = hashlib.sha256(source_manifest.encode()).hexdigest()
    connection.execute(
        """
INSERT INTO game_metadata_revisions(
  id,game_id,title,description,developer,publisher,genre,players,release_year,source_kind,created_at_ms
) VALUES(?,?,'Unsupported Puzzle','Negative netplay acceptance fixture','','','',1,NULL,'ADMIN_EDIT',?)
""",
        (metadata_id, game_id, NOW_MS),
    )
    connection.execute(
        """
INSERT INTO game_content_revisions(
  id,game_id,content_kind,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms
) VALUES(?,?,'SINGLE_FILE','ADMIN_REPLACE','unsupported',?,?,?)
""",
        (content_id, game_id, source_manifest, source_digest, NOW_MS),
    )
    connection.execute(
        """
INSERT INTO games(
  id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,
  search_text,version,created_at_ms,updated_at_ms
) VALUES(?,'01980000-0000-7000-8000-000000000005','PUBLISHED',?,?,'unsupported puzzle',1,?,?)
""",
        (game_id, metadata_id, content_id, NOW_MS, NOW_MS),
    )
    connection.execute(
        """
INSERT INTO game_content_files(game_content_revision_id,role,logical_name,blob_id,sort_order)
VALUES(?,'CONTENT','unsupported.gba','01980000-0000-7000-8000-00000000c106',0)
""",
        (content_id,),
    )


def seed_persistent_saves(connection: sqlite3.Connection, data_dir: Path) -> None:
    host_profile_id = connection.execute(
        "SELECT profile_id FROM users WHERE username='test'",
    ).fetchone()[0]
    artifact_id = connection.execute(
        "SELECT core_artifact_id FROM game_variant_revisions WHERE id=?",
        ("01980000-0000-7000-8000-00000000c105",),
    ).fetchone()[0]
    fixtures = (
        (host_profile_id, "01", b"host-save-before-netplay-v1"),
        ("01980000-0000-7000-8000-00000000a002", "02", b"guest-save-before-netplay-v1"),
    )
    for profile_id, suffix, contents in fixtures:
        blob_id = f"01980000-0000-7000-8000-00000000d1{suffix}"
        launch_id = f"01980000-0000-7000-8000-00000000d2{suffix}"
        save_id = f"01980000-0000-7000-8000-00000000d3{suffix}"
        revision_id = f"01980000-0000-7000-8000-00000000d4{suffix}"
        sha256_value, md5_value, sha1_value, crc32_value, size = materialize_bytes(data_dir, contents)
        connection.execute(
            """
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES(?,?,?,?,?,?,'application/octet-stream',?)
""",
            (blob_id, sha256_value, size, md5_value, sha1_value, crc32_value, NOW_MS),
        )
        connection.execute(
            """
INSERT INTO launch_sessions(
  id,profile_id,game_id,game_variant_revision_id,core_artifact_id,return_to,credential_sha256,
  state,bootstrap_expires_at_ms,finished_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,'/games/01980000-0000-7000-8000-00000000c101',zeroblob(32),
  'FINISHED',?,?,?, ?,?)
""",
            (launch_id, profile_id, "01980000-0000-7000-8000-00000000c101",
             "01980000-0000-7000-8000-00000000c105", artifact_id,
             NOW_MS + 1_000, NOW_MS + 500, NOW_MS + 2_000, NOW_MS, NOW_MS + 500),
        )
        connection.execute(
            """
INSERT INTO persistent_saves(
  id,profile_id,game_variant_revision_id,kind,current_revision_id,version,created_at_ms,updated_at_ms
) VALUES(?,?,?,'CORE_SAVE',?,1,?,?)
""",
            (save_id, profile_id, "01980000-0000-7000-8000-00000000c105", revision_id, NOW_MS, NOW_MS),
        )
        connection.execute(
            """
INSERT INTO persistent_save_revisions(
  id,persistent_save_id,blob_id,source_launch_session_id,client_sequence,source_event,created_at_ms
) VALUES(?,?,?,?,1,'EXIT',?)
""",
            (revision_id, save_id, blob_id, launch_id, NOW_MS),
        )


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: seed-netplay.py DATABASE DATA_DIR", file=sys.stderr)
        return 2
    database = Path(sys.argv[1]).resolve()
    data_dir = Path(sys.argv[2]).resolve()
    connection = sqlite3.connect(database)
    connection.execute("PRAGMA foreign_keys=ON")
    try:
        connection.execute("BEGIN")
        connection.execute("PRAGMA defer_foreign_keys=ON")
        for user in USERS:
            seed_user(connection, user)
        for game in GAMES:
            seed_game(connection, data_dir, game)
        seed_unsupported_game(connection)
        seed_persistent_saves(connection, data_dir)
        violations = connection.execute("PRAGMA foreign_key_check").fetchall()
        if violations:
            raise RuntimeError(f"NETPLAY_SEED_FOREIGN_KEY_VIOLATION:{violations!r}")
        connection.commit()
    except Exception:
        connection.rollback()
        raise
    finally:
        connection.close()
    print("netplay_seed=passed")
    print("accounts=test,alice,charlie")
    print("profiles=fceumm-423-v1,fbneo-423-v1")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
