#!/usr/bin/env python3
"""Classify whether a pull request changes anything the product browser exercises."""
from __future__ import annotations

import subprocess
import sys


def documentation_only(paths: list[str]) -> bool:
    # An empty or unreadable change set is treated as a product change.
    return bool(paths) and all(path.endswith(".md") or path.startswith("docs/") for path in paths)


def changed_paths(base: str, head: str) -> list[str]:
    result = subprocess.run(["git", "diff", "--name-only", "--no-renames", base, head],
                            check=True, capture_output=True, text=True)
    return [line for line in result.stdout.splitlines() if line]


def main() -> int:
    if len(sys.argv) != 3:
        raise SystemExit("usage: ci_scope.py <base> <head>")
    product = not documentation_only(changed_paths(sys.argv[1], sys.argv[2]))
    print(f"product={'true' if product else 'false'}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
