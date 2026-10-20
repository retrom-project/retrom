"""Import one verified prepared input set (tool archive and Provider chunks) into shared cache."""
from __future__ import annotations

import argparse
import json
import shutil
import tempfile
from pathlib import Path

if __package__:
    from scripts.runtime_input_manifest import artifact_records, load_manifest
    from scripts.runtime_input_transport import artifact
    from scripts.runtime_inputs import verify_prepared
    from scripts.runtime_provider_sync import sync_providers
else:
    from runtime_input_manifest import artifact_records, load_manifest
    from runtime_input_transport import artifact
    from runtime_inputs import verify_prepared
    from runtime_provider_sync import sync_providers


def import_provider_cache(source_root: Path, cache_root: Path) -> dict:
    manifest = load_manifest(source_root / "runtime-inputs.json")
    # Verify the whole input set first: damage cannot publish a partial imported set.
    verify_prepared(source_root, manifest)
    for record in artifact_records(manifest):
        artifact(record, cache_root, source_root / "archives", "")
    cache_root.mkdir(parents=True, exist_ok=True)
    # The source's installed bundles seed every chunk; no transport is configured, so nothing downloads.
    staging = Path(tempfile.mkdtemp(prefix=".import-", dir=cache_root))
    try:
        result = sync_providers(source_root / "runtime-inputs.json", staging / "providers",
                                cache=cache_root / "chunks", seeds=[source_root / "providers"])
    finally:
        shutil.rmtree(staging, ignore_errors=True)
    return {"cacheRoot": str(cache_root.resolve()), "inputSets": 1, "archives": len(artifact_records(manifest)),
            "chunks": result["SeededChunks"] + result["DownloadedChunks"]}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-root", type=Path, required=True)
    parser.add_argument("--cache-root", type=Path, required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(import_provider_cache(args.source_root, args.cache_root), sort_keys=True))
    except (OSError, ValueError) as error:
        parser.exit(1, f"{error}\n")
