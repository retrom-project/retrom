"""Reject database isolation above REPEATABLE READ in application inputs."""

from pathlib import Path
import re
import subprocess
import sys


FORBIDDEN = re.compile(r"\b(?:Level)?(?:Serializable|Linearizable)\b", re.IGNORECASE)
SOURCE_SUFFIXES = {".go", ".sql", ".py", ".sh", ".yaml", ".yml", ".json", ".toml", ".env", ".ts", ".tsx", ".js", ".mjs"}
# Only the detector and its negative fixtures may describe prohibited inputs.
DETECTOR_FILES = {"scripts/database_isolation.py", "scripts/test_database_isolation.py"}


def violations(source: str) -> list[int]:
    return [source.count("\n", 0, match.start()) + 1 for match in FORBIDDEN.finditer(source)]


def check(root: Path) -> list[str]:
    names = subprocess.check_output(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=root,
    ).decode().split("\0")
    failures = []
    for name in sorted(set(names)):
        path = root / name
        if name in DETECTOR_FILES or not path.is_file():
            continue
        if path.suffix not in SOURCE_SUFFIXES and path.name not in {"Makefile", "Dockerfile"}:
            continue
        for line in violations(path.read_text(encoding="utf-8")):
            failures.append(f"{name}:{line}: database isolation must not exceed REPEATABLE READ")
    return failures


def main() -> int:
    failures = check(Path(__file__).resolve().parents[1])
    if failures:
        print("\n".join(failures), file=sys.stderr)
        return 1
    print("database isolation: REPEATABLE READ ceiling verified")
    return 0


if __name__ == "__main__":
    sys.exit(main())
