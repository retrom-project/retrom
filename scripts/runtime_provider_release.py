"""Explicitly pin a real release's complete paired input metadata."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import tempfile

if __package__:
    from scripts.runtime_input_manifest import MAX_METADATA, REPOSITORY, TAG, validate_manifest
    from scripts.runtime_inputs import prepare
    from scripts.runtime_input_transport import transport_base
    from scripts.runtime_provider_io import _fetch_bytes, _write_json_atomic
    from scripts.runtime_provider_cache import default_cache_root
else:
    from runtime_input_manifest import MAX_METADATA, REPOSITORY, TAG, validate_manifest
    from runtime_inputs import prepare
    from runtime_input_transport import transport_base
    from runtime_provider_io import _fetch_bytes, _write_json_atomic
    from runtime_provider_cache import default_cache_root


def pin_release(tag: str, path: Path, cache: Path, *, fetch=_fetch_bytes, node=None, base_url: str = "") -> dict:
    if not isinstance(tag, str) or TAG.fullmatch(tag) is None:
        raise ValueError("RUNTIME_INPUT_RELEASE_INVALID")
    # An optional mirror only changes transport; every digest still comes from the release metadata.
    base = transport_base({"repository": REPOSITORY, "release": None}, base_url) if base_url else \
        f"{REPOSITORY}/releases/download/{tag}"
    contents = fetch(f"{base}/runtime-inputs.json", MAX_METADATA)
    if not isinstance(contents, bytes) or len(contents) > MAX_METADATA:
        raise ValueError("RUNTIME_INPUT_MANIFEST_INVALID")
    manifest = validate_manifest(json.loads(contents))
    if manifest["release"] is None or manifest["release"]["tag"] != tag:
        raise ValueError("RUNTIME_INPUT_RELEASE_INVALID")
    # The pin is published only after the tool archive, every Provider chunk and their source closure
    # validate; the verified chunks stay in the shared cache, so later preparation is offline.
    with tempfile.TemporaryDirectory(prefix="retrom-pin-") as directory:
        root = Path(directory)
        _write_json_atomic(root / "runtime-inputs.json", manifest)
        prepare(root / "runtime-inputs.json", root / "prepared", cache, fetch=fetch, node=node, base_url=base_url)
    _write_json_atomic(path, manifest)
    return manifest


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--cache-root", type=Path, default=default_cache_root(Path(__file__).resolve().parents[1]))
    parser.add_argument("--node", default=os.environ.get("RETROM_NODE", "node"))
    parser.add_argument("--base-url", default=os.environ.get("RETROM_RUNTIME_INPUT_BASE_URL", ""))
    args = parser.parse_args()
    print(json.dumps(pin_release(args.tag, args.manifest, args.cache_root, node=args.node, base_url=args.base_url),
                     sort_keys=True))
