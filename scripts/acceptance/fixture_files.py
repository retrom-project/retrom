"""Independent domain directories for disposable SQL acceptance fixtures."""
import postgres_fixture as pg
import hashlib
import json
import shutil
import uuid
import zlib
from pathlib import Path


def file_path(database, file_record):
    root = database.data_root
    relative = json.loads(file_record)["path"]
    target = root / relative
    if not relative or Path(relative).is_absolute() or ".." in Path(relative).parts or not target.resolve().is_relative_to(root):
        raise ValueError("invalid fixture file path")
    return target


def put_owned(database, contents, owner_kind, owner_id, media_type):
    if database.info.transaction_status == pg.psycopg.pq.TransactionStatus.IDLE:
        raise ValueError("fixture files require a caller-owned transaction")
    owner = str(uuid.UUID(owner_id))
    generation = str(uuid.uuid4())
    roots = {
        "GAME": f"files/{owner[-2:]}/{owner}/content/{generation}",
        "IMPORT_ITEM": f"staging/items/{owner}/payload/content/{generation}",
        "SOURCE_IMPORT_ITEM": f"staging/sources/{owner}/{generation}",
        "UPLOAD": f"staging/uploads/{owner}/{generation}",
        "SAVE_STATE": f"saves/{owner}/{generation}",
        "SCRAPE_RUN": f"scrapes/{owner}/{generation}",
        "PROVIDER_RESPONSE": f"responses/{owner}",
        "BIOS_INSTALLATION": f"bios/{owner}",
    }
    record = {"path": roots[owner_kind] + "/payload", "sha256": hashlib.sha256(contents).hexdigest(),
              "md5": hashlib.md5(contents).hexdigest(), "sha1": hashlib.sha1(contents).hexdigest(),
              "crc32": f"{zlib.crc32(contents):08x}", "size_bytes": len(contents), "media_type": media_type}
    encoded = json.dumps(record, separators=(",", ":"))
    target = file_path(database, encoded)
    target.parent.mkdir(parents=True, exist_ok=True)
    with target.open("xb") as output:
        output.write(contents)
    target.chmod(0o600)
    return encoded


def copy_owned(database, file_record, owner_kind, owner_id):
    metadata = json.loads(file_record)
    contents = file_path(database, file_record).read_bytes()
    if hashlib.sha256(contents).hexdigest() != metadata["sha256"]:
        raise ValueError("source file digest mismatch")
    return put_owned(database, contents, owner_kind, owner_id, metadata["media_type"])


def own_rows(database, table, key, value, kind, owner, columns=("file_record",), extra=""):
    rows = database.execute(f"SELECT ctid,{','.join(columns)} FROM {table} WHERE {key}=%s {extra}", (value,)).fetchall()
    copies = {}
    for row in rows:
        new_records = []
        for old in row[1:]:
            if old is not None and old not in copies:
                copies[old] = copy_owned(database, old, kind, owner)
            new_records.append(copies.get(old))
        database.execute(f"UPDATE {table} SET {','.join(column+'=%s' for column in columns)} WHERE ctid=%s", (*new_records, row[0]))
    return copies


def retire_save(database, predicate, args):
    root = database.data_root
    for (owner,) in database.execute(f"SELECT id FROM save_states WHERE {predicate}", args).fetchall():
        directory = root / "saves" / str(uuid.UUID(owner))
        if directory.exists():
            shutil.rmtree(directory)
