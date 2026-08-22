#!/usr/bin/env python3
"""Fast, deterministic source-size and lint-suppression policy gate."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from dataclasses import dataclass
from datetime import date
from pathlib import Path, PurePosixPath
from typing import Iterable


GO_PRODUCTION_LIMIT = 1000
GO_TEST_LIMIT = 1200
WEB_PRODUCTION_LIMIT = 600
WEB_TEST_LIMIT = 800
WEB_CSS_LIMIT = 800

GO_SUFFIXES = {".go"}
WEB_SUFFIXES = {".ts", ".tsx", ".js", ".jsx", ".mjs"}
STRUCTURAL_GO_LINTERS = {
    "dupl",
    "funlen",
    "gocognit",
    "gocyclo",
    "lll",
    "nestif",
}
GENERATED_FILES = {
    "internal/httpapi/generated/api.gen.go": re.compile(
        rb"Code generated .* DO NOT EDIT\."
    ),
    "web/lib/api/generated/schema.d.ts": re.compile(rb"auto-generated", re.I),
    "web/next-env.d.ts": re.compile(rb"This file should not be edited", re.I),
}
GENERATED_MARKERS = (
    re.compile(rb"Code generated .* DO NOT EDIT\."),
    re.compile(rb"auto-generated", re.I),
    re.compile(rb"This file should not be edited", re.I),
)
GO_SUPPRESSION = re.compile(r"//\s*nolint(?::([^\s/]+))?")
FE_SUPPRESSION = re.compile(
    r"eslint-disable(?:-next-line|-line)?|@ts-ignore|@ts-expect-error"
)


@dataclass(frozen=True)
class Classification:
    category: str
    line_limit: int
    generated: bool = False


@dataclass(frozen=True)
class Violation:
    path: str
    line: int
    rule: str
    message: str
    actual: int | None = None
    limit: int | None = None

    def render(self) -> str:
        measurement = ""
        if self.actual is not None and self.limit is not None:
            measurement = f" actual={self.actual} limit={self.limit}"
        return f"{self.path}:{self.line}: {self.rule}:{measurement} {self.message}".rstrip()


@dataclass(frozen=True)
class AllowlistEntry:
    path: str
    line: int
    symbol: str
    linter: str
    reason: str
    invariant: str
    review_after: date

    @property
    def key(self) -> tuple[str, int, str]:
        return self.path, self.line, self.linter


def count_physical_lines(contents: bytes) -> int:
    if not contents:
        return 0
    return contents.count(b"\n") + (0 if contents.endswith(b"\n") else 1)


def classify_path(relative_path: str) -> Classification | None:
    path = PurePosixPath(relative_path)
    normalized = path.as_posix()
    if normalized in GENERATED_FILES:
        return Classification("generated", 0, generated=True)
    if path.suffix in GO_SUFFIXES:
        if path.name.endswith("_test.go"):
            return Classification("go-test", GO_TEST_LIMIT)
        return Classification("go-production", GO_PRODUCTION_LIMIT)
    if not path.parts or path.parts[0] != "web":
        return None
    if path.suffix == ".css":
        return Classification("web-css", WEB_CSS_LIMIT)
    if path.suffix not in WEB_SUFFIXES:
        return None
    is_test = "e2e" in path.parts or ".test." in path.name or ".spec." in path.name
    if is_test:
        return Classification("web-test", WEB_TEST_LIMIT)
    return Classification("web-production", WEB_PRODUCTION_LIMIT)


def discover_repository_files(root: Path) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=root,
        check=True,
        capture_output=True,
    )
    discovered = (
        entry.decode("utf-8")
        for entry in result.stdout.split(b"\0")
        if entry
    )
    return sorted(
        entry
        for entry in discovered
        if (root / entry).exists() or (root / entry).is_symlink()
    )


def _config_violation(message: str) -> Violation:
    return Violation(
        "quality/go-suppressions.json",
        1,
        "invalid-allowlist",
        message,
    )


def _load_allowlist(root: Path) -> tuple[list[AllowlistEntry], list[Violation]]:
    path = root / "quality" / "go-suppressions.json"
    try:
        raw = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        return [], [_config_violation(f"cannot read closed suppression policy: {error}")]
    if not isinstance(raw, dict) or set(raw) != {"version", "exceptions"}:
        return [], [_config_violation("top level must contain only version and exceptions")]
    if raw["version"] != 1 or not isinstance(raw["exceptions"], list):
        return [], [_config_violation("version must be 1 and exceptions must be an array")]

    required = {
        "path",
        "line",
        "symbol",
        "linter",
        "reason",
        "invariant",
        "reviewAfter",
    }
    entries: list[AllowlistEntry] = []
    violations: list[Violation] = []
    seen: set[tuple[str, int, str]] = set()
    for index, item in enumerate(raw["exceptions"]):
        label = f"exceptions[{index}]"
        if not isinstance(item, dict) or set(item) != required:
            violations.append(_config_violation(f"{label} has an unknown or missing field"))
            continue
        string_fields = required - {"line"}
        if (
            not isinstance(item["line"], int)
            or item["line"] < 1
            or any(not isinstance(item[field], str) or not item[field].strip() for field in string_fields)
        ):
            violations.append(_config_violation(f"{label} contains an invalid or empty value"))
            continue
        if PurePosixPath(item["path"]).is_absolute() or ".." in PurePosixPath(item["path"]).parts:
            violations.append(_config_violation(f"{label}.path must be repository relative"))
            continue
        if item["linter"] in STRUCTURAL_GO_LINTERS or "," in item["linter"]:
            violations.append(_config_violation(f"{label}.linter is structural or bundled"))
            continue
        try:
            review_after = date.fromisoformat(item["reviewAfter"])
        except ValueError:
            violations.append(_config_violation(f"{label}.reviewAfter must be YYYY-MM-DD"))
            continue
        entry = AllowlistEntry(
            path=item["path"],
            line=item["line"],
            symbol=item["symbol"],
            linter=item["linter"],
            reason=item["reason"],
            invariant=item["invariant"],
            review_after=review_after,
        )
        if entry.key in seen:
            violations.append(_config_violation(f"{label} duplicates {entry.key}"))
            continue
        seen.add(entry.key)
        entries.append(entry)
    return entries, violations


def _first_lines(contents: bytes, count: int = 12) -> bytes:
    return b"\n".join(contents.splitlines()[:count])


def _generated_violations(
    path: str, contents: bytes, classification: Classification
) -> list[Violation]:
    header = _first_lines(contents)
    if classification.generated:
        marker = GENERATED_FILES[path]
        if not marker.search(header):
            return [
                Violation(
                    path,
                    1,
                    "generated-marker",
                    "allowlisted generated file is missing its strict generator marker",
                )
            ]
        return []
    if any(marker.search(header) for marker in GENERATED_MARKERS):
        return [
            Violation(
                path,
                1,
                "generated-marker",
                "generated marker is not accepted outside the exact generated-file allowlist",
            )
        ]
    return []


def _go_suppressions(path: str, text: str) -> Iterable[tuple[int, list[str]]]:
    for line_number, line in enumerate(text.splitlines(), start=1):
        for match in GO_SUPPRESSION.finditer(line):
            linters = [] if match.group(1) is None else match.group(1).split(",")
            yield line_number, linters


def _go_suppression_violations(
    path: str,
    text: str,
    allowlist: dict[tuple[str, int, str], AllowlistEntry],
    used: set[tuple[str, int, str]],
    today: date,
) -> list[Violation]:
    violations: list[Violation] = []
    for line_number, linters in _go_suppressions(path, text):
        if not linters:
            violations.append(
                Violation(path, line_number, "multi-linter-suppression", "//nolint must name exactly one reviewed linter")
            )
            continue
        if any(linter in STRUCTURAL_GO_LINTERS for linter in linters):
            violations.append(
                Violation(path, line_number, "forbidden-go-suppression", "structural lint rules cannot be suppressed")
            )
            continue
        if len(linters) != 1:
            violations.append(
                Violation(path, line_number, "multi-linter-suppression", "suppressions must name one linter")
            )
        linter = linters[0]
        key = (path, line_number, linter)
        entry = allowlist.get(key)
        if entry is None:
            violations.append(
                Violation(path, line_number, "unregistered-go-suppression", f"//nolint:{linter} is absent from the central allowlist")
            )
            continue
        used.add(key)
        if entry.review_after < today:
            violations.append(
                Violation(path, line_number, "expired-allowlist-entry", f"//nolint:{linter} review expired on {entry.review_after.isoformat()}")
            )
    return violations


def _frontend_suppression_violations(path: str, text: str) -> list[Violation]:
    violations: list[Violation] = []
    for line_number, line in enumerate(text.splitlines(), start=1):
        if FE_SUPPRESSION.search(line):
            violations.append(
                Violation(
                    path,
                    line_number,
                    "forbidden-fe-suppression",
                    "frontend inline disable and TypeScript ignore directives are forbidden",
                )
            )
    return violations


def validate_repository(root: Path, *, today: date | None = None) -> list[Violation]:
    current_date = date.today() if today is None else today
    entries, violations = _load_allowlist(root)
    allowlist = {entry.key: entry for entry in entries}
    used: set[tuple[str, int, str]] = set()

    for relative_path in discover_repository_files(root):
        classification = classify_path(relative_path)
        if classification is None:
            continue
        path = root / relative_path
        try:
            contents = path.read_bytes()
        except OSError as error:
            violations.append(Violation(relative_path, 1, "source-read", str(error)))
            continue
        violations.extend(_generated_violations(relative_path, contents, classification))
        if classification.generated:
            continue
        line_count = count_physical_lines(contents)
        if line_count > classification.line_limit:
            violations.append(
                Violation(
                    relative_path,
                    1,
                    "max-lines",
                    f"{classification.category} file exceeds its physical-line limit",
                    actual=line_count,
                    limit=classification.line_limit,
                )
            )
        text = contents.decode("utf-8", errors="replace")
        if relative_path.endswith(".go"):
            violations.extend(
                _go_suppression_violations(
                    relative_path, text, allowlist, used, current_date
                )
            )
        elif PurePosixPath(relative_path).suffix in WEB_SUFFIXES:
            violations.extend(_frontend_suppression_violations(relative_path, text))

    for entry in entries:
        if entry.key not in used:
            violations.append(
                Violation(
                    entry.path,
                    entry.line,
                    "unused-allowlist-entry",
                    f"allowlist entry for {entry.linter} has no matching source suppression",
                )
            )
        elif entry.review_after < current_date and not any(
            violation.path == entry.path
            and violation.line == entry.line
            and violation.rule == "expired-allowlist-entry"
            for violation in violations
        ):
            violations.append(
                Violation(
                    entry.path,
                    entry.line,
                    "expired-allowlist-entry",
                    f"allowlist review expired on {entry.review_after.isoformat()}",
                )
            )
    return sorted(violations, key=lambda item: (item.path, item.line, item.rule))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    arguments = parser.parse_args(argv)
    try:
        violations = validate_repository(arguments.root.resolve())
    except subprocess.CalledProcessError as error:
        print(f"quality-structure-check: git file discovery failed: {error}", file=sys.stderr)
        return 2
    for violation in violations:
        print(violation.render())
    if violations:
        print(f"quality-structure-check: {len(violations)} violation(s)", file=sys.stderr)
        return 1
    print("quality-structure-check: PASS")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
