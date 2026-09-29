"""Validate ordinary patches and proposed immutable upstream baselines."""

import re
import subprocess
from pathlib import Path


def valid_core_ancestry(root: Path, branch: str, fork: dict) -> bool:
    if not branch.startswith("sync/upstream-"):
        return _ancestor(root, fork["defaultBranch"])
    baseline = branch.removeprefix("sync/upstream-")
    if not re.fullmatch(r"g[0-9a-f]{12}", baseline):
        return _ancestor(root, fork["defaultBranch"])
    if fork["defaultBranch"] != "retrom/" + baseline:
        return False
    upstreams = fork.get("upstreams")
    if not isinstance(upstreams, list) or len(upstreams) != 1 or not isinstance(upstreams[0], dict):
        return False
    upstream = upstreams[0]
    commit = upstream.get("commit")
    if not isinstance(commit, str) or not re.fullmatch(r"[0-9a-f]{40}", commit):
        return False
    if upstream.get("refType") != "COMMIT" or upstream.get("ref") != commit or baseline != "g" + commit[:12]:
        return False
    if not _ancestor(root, commit):
        return False
    result = subprocess.run(["git", "-C", str(root), "rev-list", "--min-parents=2", commit + "..HEAD"],
                            capture_output=True, text=True, check=False)
    return result.returncode == 0 and not result.stdout.strip()


def _ancestor(root: Path, commit: str) -> bool:
    return subprocess.run(["git", "-C", str(root), "merge-base", "--is-ancestor", commit, "HEAD"],
                          capture_output=True, check=False).returncode == 0
