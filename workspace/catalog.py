"""Versioned development repository catalog; standard-library-only bootstrap API."""
from __future__ import annotations

import json
import sys
from pathlib import Path

ALLOWED_ROLES = {"application", "runtime", "core", "support"}


def parse_manifest(text: str) -> list[dict[str, object]]:
    data = json.loads(text)
    if not isinstance(data, dict) or data.get("schemaVersion") != 1:
        raise ValueError("dependency manifest schemaVersion must be 1")
    repositories = data.get("repositories")
    if not isinstance(repositories, list) or not repositories:
        raise ValueError("dependency repositories must be a non-empty list")
    validate_repositories(repositories)
    for repo in repositories:
        if repo["id"] == "retrom" or repo["role"] == "application":
            raise ValueError("Retrom belongs only in the bootstrap manifest")
        path = Path(repo["path"])
        role = repo["role"]
        valid = (role == "runtime" and repo["id"] == "retrom-runtime" and path.parts == ("project", "retrom-runtime"))
        valid |= (role in {"core", "support"} and len(path.parts) == 3 and path.parts[:2] == ("project", "retrom-core" if role == "core" else "retrom-other"))
        if not valid or path.as_posix() != repo["path"]:
            raise ValueError(f"invalid repository layout: {repo['path']}")
    if not any(repo["id"] == "retrom-runtime" for repo in repositories):
        raise ValueError("dependency manifest must contain retrom-runtime")
    pending = ["retrom-runtime"]
    reachable: set[str] = set()
    by_id = {repo["id"]: repo for repo in repositories}
    while pending:
        repo_id = pending.pop()
        if repo_id not in reachable:
            reachable.add(repo_id)
            pending.extend(by_id[repo_id]["dependsOn"])
    if reachable != set(by_id):
        raise ValueError(f"repositories not reachable from retrom-runtime: {sorted(set(by_id) - reachable)}")
    return repositories


def validate_repositories(repositories: list[object]) -> None:
    ids: set[str] = set()
    paths: set[str] = set()

    for index, value in enumerate(repositories):
        if not isinstance(value, dict):
            raise ValueError(f"repositories[{index}] must be an object")
        repo = value
        required = {
            "id": str,
            "path": str,
            "role": str,
            "gitlink": str,
            "defaultBranch": str,
            "submodules": bool,
            "dependsOn": list,
        }
        for field, expected_type in required.items():
            if field not in repo or not isinstance(repo[field], expected_type):
                raise ValueError(
                    f"repositories[{index}].{field} must be {expected_type.__name__}"
                )
        if "shallowClone" in repo and not isinstance(repo["shallowClone"], bool):
            raise ValueError(
                f"repositories[{index}].shallowClone must be bool"
            )

        repo_id = str(repo["id"])
        repo_path = str(repo["path"])
        if not repo_id or repo_id in ids:
            raise ValueError(f"duplicate or empty repository id: {repo_id!r}")
        if not repo_path or repo_path in paths:
            raise ValueError(f"duplicate or empty repository path: {repo_path!r}")
        ids.add(repo_id)
        paths.add(repo_path)

        relative = Path(repo_path)
        if relative.is_absolute() or ".." in relative.parts or relative.parts[:1] != ("project",):
            raise ValueError(f"repository path must stay under project/: {repo_path}")
        if str(repo["role"]) not in ALLOWED_ROLES:
            raise ValueError(f"unsupported role for {repo_id}: {repo['role']}")
        if any(not repo[field].strip() or repo[field].startswith("-") for field in ("gitlink", "defaultBranch")):
            raise ValueError(f"gitlink and defaultBranch are required for {repo_id}")
        if repo["defaultBranch"].startswith(("codex/", "feat/", "fix/", "feature/")):
            raise ValueError(f"defaultBranch must be a maintenance branch, not a PFB branch: {repo_id}")
        dependencies = repo["dependsOn"]
        if any(not isinstance(item, str) or not item for item in dependencies):
            raise ValueError(f"dependsOn must contain repository ids for {repo_id}")

    by_id = {str(repo["id"]): repo for repo in repositories if isinstance(repo, dict)}
    for repo_id, repo in by_id.items():
        unknown = set(repo["dependsOn"]) - set(by_id)
        if unknown:
            raise ValueError(f"unknown dependencies for {repo_id}: {sorted(unknown)}")

    visiting: set[str] = set()
    visited: set[str] = set()

    def visit(repo_id: str) -> None:
        if repo_id in visiting:
            raise ValueError(f"dependency cycle includes {repo_id}")
        if repo_id in visited:
            return
        visiting.add(repo_id)
        for dependency in by_id[repo_id]["dependsOn"]:
            visit(str(dependency))
        visiting.remove(repo_id)
        visited.add(repo_id)

    for repo_id in by_id:
        visit(repo_id)


if __name__ == "__main__":
    try:
        repositories = parse_manifest(Path(__file__).with_name("manifest.yaml").read_text())
        print(f"workspace manifest valid: {len(repositories)} dependencies")
    except (OSError, ValueError) as error:
        print(f"workspace manifest error: {error}", file=sys.stderr)
        raise SystemExit(1)
