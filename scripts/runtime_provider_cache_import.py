"""Copy verified existing downloads into a shared cache without network access."""
from __future__ import annotations

import argparse
import json
from pathlib import Path

if __package__:
    from scripts.runtime_provider_cache import cached_download
    from scripts.runtime_provider_release import METADATA_MAX_BYTES, _parse_release_metadata
    from scripts.runtime_providers import _verify_archive_bytes
else:
    from runtime_provider_cache import cached_download
    from runtime_provider_release import METADATA_MAX_BYTES, _parse_release_metadata
    from runtime_providers import _verify_archive_bytes


def _read_bytes(path: Path, maximum: int) -> bytes:
    with path.open("rb") as source:
        return source.read(maximum + 1)


def _verify_metadata(contents: bytes, tag: str, expected: dict) -> None:
    if _parse_release_metadata(contents, tag) != expected:
        raise ValueError("PROVIDER_CACHE_METADATA_CONFLICT")


def import_provider_cache(source_root: Path, cache_root: Path) -> dict:
    source_root, cache_root = source_root.resolve(), cache_root.resolve()
    if not source_root.is_dir():
        raise ValueError("PROVIDER_CACHE_SOURCE_INVALID")
    releases, archives = 0, 0
    for descriptor in sorted((source_root / "releases").glob("*/provider-release.json")):
        contents = _read_bytes(descriptor, METADATA_MAX_BYTES)
        tag = descriptor.parent.name
        metadata = _parse_release_metadata(contents, tag)
        cached_download(
            cache_root / "releases" / tag / "provider-release.json", lambda: contents,
            lambda value: _verify_metadata(value, tag, metadata), METADATA_MAX_BYTES,
        )
        releases += 1
        for provider in metadata["providers"]:
            relative = Path(provider["providerId"]) / f'{provider["bundleSha256"]}.tar.gz'
            archive = source_root / relative
            if not archive.is_file():
                continue
            cached_download(
                cache_root / relative, lambda: _read_bytes(archive, provider["bundleSizeBytes"]),
                lambda value: _verify_archive_bytes(value, provider), provider["bundleSizeBytes"],
            )
            archives += 1
    return {"cacheRoot": str(cache_root), "releases": releases, "archives": archives}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-root", type=Path, required=True)
    parser.add_argument("--cache-root", type=Path, required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(import_provider_cache(args.source_root, args.cache_root), sort_keys=True))
    except (OSError, ValueError) as error:
        parser.exit(1, f"{error}\n")
