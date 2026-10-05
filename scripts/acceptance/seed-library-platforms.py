#!/usr/bin/env python3
"""Seed metadata-only platform facets in disposable UI data; never launch them."""
import postgres_fixture as pg
import sys
import uuid
from pathlib import Path
from ui_layout_state import validate_database

path = validate_database(Path(sys.argv[1]))
with pg.connect(path) as db:
    db.row_factory = pg.row_factory

    template = dict(db.execute("SELECT * FROM games WHERE status='PUBLISHED' LIMIT 1").fetchone())
    instances = db.execute("SELECT min(id) FROM platform_instances WHERE enabled=1 GROUP BY platform_id").fetchall()
    for index, (instance,) in enumerate(instances):
        row = {**template, "id": str(uuid.uuid4()), "platform_instance_id": instance,
               "title": f"Platform scrollbar fixture {index}", "search_text": f"platform scrollbar fixture {index}",
               "source_manifest_json": "[]", "source_manifest_digest": f"{index + 500000:064x}"}
        columns = list(row)
        db.execute(f"INSERT INTO games({','.join(columns)}) VALUES({','.join('%s' for _ in columns)})", list(row.values()))
    print(len(instances))
