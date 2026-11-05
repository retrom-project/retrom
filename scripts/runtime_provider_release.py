"""Explicitly pin a real release's paired Provider metadata and its Go host module."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile

if __package__:
    from scripts.runtime_input_manifest import MAX_METADATA, MODULE, REPOSITORY, TAG, check_module_pin, validate_manifest
    from scripts.runtime_inputs import prepare
    from scripts.runtime_input_transport import transport_base
    from scripts.runtime_provider_io import _fetch_bytes, _write_json_atomic
    from scripts.runtime_provider_cache import default_cache_root
else:
    from runtime_input_manifest import MAX_METADATA, MODULE, REPOSITORY, TAG, check_module_pin, validate_manifest
    from runtime_inputs import prepare
    from runtime_input_transport import transport_base
    from runtime_provider_io import _fetch_bytes, _write_json_atomic
    from runtime_provider_cache import default_cache_root


ROOT = Path(__file__).resolve().parents[1]


def pin_module(tag: str) -> None:
    """Pin the embedded runtime host module to the same immutable tag; go.sum records its checksum."""
    subprocess.run(["go", "get", f"{MODULE}@{tag}"], cwd=ROOT, check=True)
    check_module_pin({"release": {"tag": tag}}, (ROOT / "go.mod").read_text(encoding="utf-8"))


def pin_release(tag: str, path: Path, cache: Path, *, fetch=_fetch_bytes, base_url: str = "",
                pin_go_module=pin_module) -> dict:
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
    # The pin is published only after every Provider chunk and its source closure validate; the verified chunks
    # stay in the shared cache, so later preparation is offline.
    with tempfile.TemporaryDirectory(prefix="retrom-pin-") as directory:
        root = Path(directory)
        _write_json_atomic(root / "runtime-inputs.json", manifest)
        prepare(root / "runtime-inputs.json", root / "prepared", cache, base_url=base_url)
    pin_go_module(tag)
    _write_json_atomic(path, manifest)
    return manifest


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--cache-root", type=Path, default=default_cache_root(Path(__file__).resolve().parents[1]))
    parser.add_argument("--base-url", default=os.environ.get("RETROM_RUNTIME_INPUT_BASE_URL", ""))
    args = parser.parse_args()
    print(json.dumps(pin_release(args.tag, args.manifest, args.cache_root, base_url=args.base_url),
                     sort_keys=True))
