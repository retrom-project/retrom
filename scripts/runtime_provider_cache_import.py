"""Import the three verified archives of one prepared input set into shared cache."""
from __future__ import annotations

import argparse
import json
from pathlib import Path

if __package__:
    from scripts.runtime_input_manifest import artifact_records, load_manifest
    from scripts.runtime_input_transport import artifact
    from scripts.runtime_inputs import verify_prepared
else:
    from runtime_input_manifest import artifact_records, load_manifest
    from runtime_input_transport import artifact
    from runtime_inputs import verify_prepared


def import_provider_cache(source_root: Path, cache_root: Path) -> dict:
    manifest = load_manifest(source_root / "runtime-inputs.json")
    # Verify the whole input set first: damage cannot publish a partial imported set.
    verify_prepared(source_root, manifest)
    for record in artifact_records(manifest):
        artifact(record, cache_root, source_root / "archives", "")
    return {"cacheRoot": str(cache_root.resolve()), "inputSets": 1, "archives": 3}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-root", type=Path, required=True)
    parser.add_argument("--cache-root", type=Path, required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(import_provider_cache(args.source_root, args.cache_root), sort_keys=True))
    except (OSError, ValueError) as error:
        parser.exit(1, f"{error}\n")
