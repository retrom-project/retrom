"""Own a disposable PostgreSQL database together with its acceptance files."""
import json
import os
import re
import sys
import uuid
from contextlib import contextmanager
from pathlib import Path
from urllib.parse import urlsplit, urlunsplit

import psycopg
from psycopg import sql


class Row(tuple):
    """Fixture rows support positional scans and named copy operations."""

    def __new__(cls, values, names):
        row = super().__new__(cls, values)
        row.names = names
        return row

    def keys(self):
        return self.names

    def __getitem__(self, key):
        return super().__getitem__(self.names.index(key) if isinstance(key, str) else key)


def row_factory(cursor):
    names = tuple(column.name for column in cursor.description) if cursor.description else ()
    return lambda values: Row(values, names)


class Connection(psycopg.Connection):
    data_root: Path


def validate_database(path: Path) -> Path:
    root = Path(path).resolve()
    if root.name != "data" or not root.parent.name.startswith(("retrom-ui-acceptance.", "retrom-web-e2e.")):
        raise ValueError("seed requires the disposable acceptance data directory")
    return root


def descriptor(root):
    return validate_database(root) / "postgres-test.json"


def database_url(root):
    location = json.loads(descriptor(root).read_text())["databaseUrl"]
    if not re.fullmatch(r"/retrom_acceptance_[0-9a-f]{32}", urlsplit(location).path):
        raise ValueError("database is not an acceptance database")
    return location


def connect(root, *, readonly=False):
    database = Connection.connect(database_url(root), row_factory=row_factory, connect_timeout=10)
    database.data_root = validate_database(root)
    database.read_only = readonly
    database.isolation_level = psycopg.IsolationLevel.REPEATABLE_READ
    return database


def create(root):
    path = descriptor(root)
    if path.exists():
        raise ValueError("acceptance database already exists")
    base = os.environ["RETROM_TEST_DATABASE_URL"]
    parsed = urlsplit(base)
    if parsed.scheme not in ("postgres", "postgresql"):
        raise ValueError("RETROM_TEST_DATABASE_URL must be a PostgreSQL URL")
    name = "retrom_acceptance_" + uuid.uuid4().hex
    location = urlunsplit(parsed._replace(path="/" + name))
    path.parent.mkdir(parents=True, exist_ok=True)
    with psycopg.connect(base, autocommit=True) as admin:
        admin.execute(sql.SQL("CREATE DATABASE {} TEMPLATE template0").format(sql.Identifier(name)))
        try:
            with open(path, "x", opener=lambda name, flags: os.open(name, flags, 0o600)) as output:
                json.dump({"databaseUrl": location}, output)
        except Exception:
            admin.execute(sql.SQL("DROP DATABASE {} WITH (FORCE)").format(sql.Identifier(name)))
            raise
    return location


def drop(root):
    path = descriptor(root)
    if not path.exists():
        return
    location = urlsplit(database_url(root))
    base = os.environ["RETROM_TEST_DATABASE_URL"]
    admin_location = urlsplit(base)
    if location.netloc != admin_location.netloc:
        raise ValueError("acceptance database belongs to another server")
    with psycopg.connect(base, autocommit=True) as admin:
        admin.execute(sql.SQL("DROP DATABASE {} WITH (FORCE)").format(sql.Identifier(location.path[1:])))
    path.unlink()


def query(root, contents):
    with connect(root) as database:
        cursor = database.execute(contents)
        if cursor.description:
            for row in cursor:
                print("|".join("" if value is None else str(value) for value in row))


@contextmanager
def disposable(root):
    create(root)
    try:
        yield root
    finally:
        drop(root)


if __name__ == "__main__":
    command, directory = sys.argv[1], Path(sys.argv[2])
    if command == "create":
        print(create(directory))
    elif command == "drop":
        drop(directory)
    elif command == "url":
        print(database_url(directory))
    elif command == "query":
        query(directory, sys.argv[3] if len(sys.argv) > 3 else sys.stdin.read())
    else:
        raise SystemExit("unknown PostgreSQL fixture command")
