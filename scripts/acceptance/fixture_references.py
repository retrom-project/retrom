"""Explicit reference deltas for disposable SQL-only layout fixtures.

These fixtures copy already protected content or add/remove flat screenshots.
Archive lifetime transitions must go through the real product API. Zero counts
are picked up by the running worker's normal reconciliation, including on restart.
"""
from collections import Counter


def adjust_references(database, rows, direction=1):
    if not database.in_transaction or direction not in (-1, 1):
        raise ValueError("fixture references require a caller-owned transaction")
    counts = Counter(blob for row in rows for blob in row if blob is not None)
    for blob, count in counts.items():
        found = database.execute("SELECT ref_count FROM blobs WHERE id=?", (blob,)).fetchone()
        if found is None:
            raise ValueError("fixture references a missing Blob")
        before = found[0]
        after = before + direction * count
        if after < 0 or after > 2**63 - 1:
            raise ValueError("fixture reference count is out of range")
        if (before == 0) != (after == 0) and database.execute(
            "SELECT 1 FROM archive_entries WHERE archive_blob_id=? LIMIT 1", (blob,),
        ).fetchone():
            raise ValueError("archive lifetime fixtures must use the product API")
        changed = database.execute(
            "UPDATE blobs SET ref_count=? WHERE id=? AND ref_count=?", (after, blob, before),
        ).rowcount
        if changed != 1:
            raise ValueError("fixture reference count changed concurrently")
        if after > 0:
            database.execute("DELETE FROM blob_gc_candidates WHERE blob_id=?", (blob,))
