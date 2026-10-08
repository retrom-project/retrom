"""Prepare the repository's pinned input set; transport never chooses its identity."""
from __future__ import annotations

import argparse
import os
import subprocess
from pathlib import Path

try:
    from .runtime_inputs import prepare
    from .runtime_provider_cache import default_cache_root
except ImportError:
    from runtime_inputs import prepare
    from runtime_provider_cache import default_cache_root

ROOT = Path(__file__).resolve().parents[1]


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive-root", type=Path, default=None)
    parser.add_argument("--base-url", default=os.environ.get("RETROM_RUNTIME_INPUT_BASE_URL", ""))
    parser.add_argument("--output-root", type=Path, default=ROOT / ".cache/runtime-inputs")
    parser.add_argument("--cache-root", type=Path, default=default_cache_root(ROOT))
    parser.add_argument("--node", default=os.environ.get("RETROM_NODE", "node"))
    parser.add_argument("--run", nargs=argparse.REMAINDER, help="execute a command with this verified input set")
    arguments = parser.parse_args()
    archive_root = arguments.archive_root
    if archive_root is None and os.environ.get("RETROM_RUNTIME_INPUT_ARCHIVE_ROOT"):
        archive_root = Path(os.environ["RETROM_RUNTIME_INPUT_ARCHIVE_ROOT"])
    prepared = prepare(ROOT / "data/runtime-inputs.json", arguments.output_root, arguments.cache_root, archive_root=archive_root,
                       base_url=arguments.base_url, node=arguments.node)
    values = {"RETROM_RUNTIME_TOOL_INPUT": str(prepared / "tool"), "RETROM_PROVIDER_INPUT": str(prepared / "providers"),
              "RETROM_RUNTIME_INPUT_MANIFEST": str((ROOT / "data/runtime-inputs.json").resolve())}
    if arguments.run:
        subprocess.run(arguments.run, env={**os.environ, **values, "RETROM_RUNTIME_ROOT": values["RETROM_RUNTIME_TOOL_INPUT"],
                                          "RETROM_PROVIDER_ROOT": values["RETROM_PROVIDER_INPUT"]}, check=True)
        return
    environment_file = os.environ.get("GITHUB_ENV")
    if environment_file:
        with Path(environment_file).open("a") as output:
            for key, value in values.items():
                output.write(key + "=" + value + "\n")
    else:
        for key, value in values.items():
            print(key + "=" + value)


if __name__ == "__main__":
    main()
