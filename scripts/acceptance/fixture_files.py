"""Independent files for disposable SQL fixtures; product lifecycle uses domain APIs."""
import hashlib
import time
import uuid
import zlib
from pathlib import Path


def file_path(database, file_id):
    root = Path(database.execute("PRAGMA database_list").fetchone()[2]).parent
    return root / "files" / file_id[:2] / file_id


def put_owned(database, contents, owner_kind, owner_id, media_type):
    if not database.in_transaction:
        raise ValueError("fixture files require a caller-owned transaction")
    file_id = str(uuid.uuid4())
    target = file_path(database, file_id)
    target.parent.mkdir(parents=True, exist_ok=True)
    with target.open("xb") as output:
        output.write(contents)
    target.chmod(0o600)
    database.execute(
        "INSERT INTO stored_files(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms,owner_kind,owner_id) "
        "VALUES(?,?,?,?,?,?,?,?,?,?)",
        (file_id, hashlib.sha256(contents).hexdigest(), len(contents), hashlib.md5(contents).hexdigest(),
         hashlib.sha1(contents).hexdigest(), f"{zlib.crc32(contents):08x}", media_type,
         time.time_ns() // 1000000, owner_kind, owner_id),
    )
    return file_id


def copy_owned(database, file_id, owner_kind, owner_id):
    metadata = database.execute("SELECT media_type,sha256 FROM stored_files WHERE id=?", (file_id,)).fetchone()
    if metadata is None:
        raise ValueError("missing source file")
    contents = file_path(database, file_id).read_bytes()
    if hashlib.sha256(contents).hexdigest() != metadata[1]:
        raise ValueError("source file digest mismatch")
    return put_owned(database, contents, owner_kind, owner_id, metadata[0])


def own_rows(database, table, key, value, kind, owner, columns=("blob_id",), extra=""):
    # All identifiers and the optional role predicate are fixed by fixture callers.
    rows = database.execute(f"SELECT rowid,{','.join(columns)} FROM {table} WHERE {key}=? {extra}", (value,)).fetchall()
    copies = {}
    for row in rows:
        new_ids = []
        for old_id in row[1:]:
            if old_id is not None and old_id not in copies:
                copies[old_id] = copy_owned(database, old_id, kind, owner)
            new_ids.append(copies.get(old_id))
        database.execute(f"UPDATE {table} SET {','.join(column+'=?' for column in columns)} WHERE rowid=?", (*new_ids, row[0]))
    return copies


def retire_save(database, predicate, args):
    database.execute(
        "UPDATE stored_files SET retired_at_ms=COALESCE(retired_at_ms,?) WHERE owner_kind='SAVE_STATE' "
        f"AND owner_id IN (SELECT id FROM save_states WHERE {predicate})", (time.time_ns() // 1000000, *args),
    )
