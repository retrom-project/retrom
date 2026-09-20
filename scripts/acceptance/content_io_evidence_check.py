"""Verify current candidate evidence through the same PFB's Runtime validator; never publish."""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import subprocess

from scripts.acceptance.content_io_host_check import ROOT, validate_paths
from scripts.pfb.spec import load_spec


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", type=Path, required=True)
    args = parser.parse_args()
    value = validate_paths(args.env, ROOT / ".pfb/workspace/content-io/evidence-check")
    spec = load_spec(ROOT)
    if spec["id"] != value["pfb"]["id"] or spec["runtime"]["root"] != value["repositories"]["runtime"]["root"]:
        raise ValueError("CONTENT_IO_EVIDENCE_PFB_MISMATCH")
    runtime = Path(spec["runtime"]["root"])
    completed = subprocess.run([value["tools"]["node"]["path"], "scripts/content-io/evidence.mjs", "--candidate", "--env", str(args.env)],
                               cwd=runtime, check=False, timeout=600)
    if completed.returncode == 0:
        print(json.dumps({"status": "CANDIDATE_VERIFIED", "pfbId": spec["id"]}))
    return completed.returncode


if __name__ == "__main__":
    raise SystemExit(main())
