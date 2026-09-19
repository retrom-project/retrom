# Catalog golden capture

[`catalog-golden.json`](catalog-golden.json) is a pre-move characterization fixture for the six catalog value types now owned by `internal/model/runtimecontract`. It was captured by running the old `internal/capability/runtime/runtimecatalog` Go implementation, before those types moved packages. It is historical evidence for serialization compatibility; it is not a generator to run when behavior changes.

## Capture provenance

- Source revision: `d709708600b0be3dc5cfe29f12bbc9f529fabefa` (`refactor(RF01): close source census before shared model migration`).
- Module/toolchain version: `go 1.26.5` in that revision's `go.mod`. The RF03 environment record also identifies the fixed Linux AMD64 toolchain as Go 1.26.5 with `GOTOOLCHAIN=local` and `GOPROXY=off`. The capture record did not retain a separate `go version` command output, so no more specific build string is asserted here.
- Fixed input: `data/runtime-target-bindings/v1/catalog.json` from that revision, SHA-256 `ad9abba0462afe68e89b894e18e938020d0c127d9e8c0c8da9b614f5b00b86d6`.
- Old entry point: `runtimecatalog.ParseCatalog`. At the capture revision it decoded with unknown-field rejection, rejected trailing JSON, validated schema version and the nonempty, sorted, unique binding set, and called `runtimecatalog.ValidateDefinitions` before returning the catalog.
- Captured production result: 82 bindings; `json.Marshal(catalog)` is 29,442 bytes with SHA-256 `77d44b594763fcf5576b196dfe9f366cb2e9be192e2cff41125b8d2063b21ee1`.
- Checked-in fixture file SHA-256: `ded88a145902fe433c225939bc204e61c395f33702b63e2d4bd4e2521cb69586`.
- The original capture program is preserved byte-for-byte as [`catalog-capture.go.txt`](catalog-capture.go.txt), SHA-256 `c8c224cff38d88da3b63f2733317e1e01328631153402466bda56e4ae610d311`.

The seven fixed samples are `zero-catalog`, `empty-catalog`, `zero-binding`, `empty-binding`, `asset-pack-unicode`, `platform`, and `core`. The zero/empty pairs lock the JSON distinction between `null` and `[]`. The Unicode samples include full-width `Ｓ`, `ß`, and `游戏`; the asset-pack and platform names also lock the standard `encoding/json` escaping of `<`, `&`, and `>`.

## Isolated reproduction

Run these commands from a current Retrom checkout. They export only the old tracked module, then place the preserved program inside that module so Go's `internal` import rule is satisfied:

```sh
revision=d709708600b0be3dc5cfe29f12bbc9f529fabefa
capture_source="$PWD/internal/model/runtimecontract/testdata/catalog-capture.go.txt"
workdir="$(mktemp -d)"
mkdir -p "$workdir/module/.cache/refactor/runtimecatalog-old-golden"
git archive "$revision" | tar -x -C "$workdir/module"
cp "$capture_source" "$workdir/module/.cache/refactor/runtimecatalog-old-golden/main.go"
cd "$workdir/module"
GOTOOLCHAIN=local GOPROXY=off go run ./.cache/refactor/runtimecatalog-old-golden "$workdir/capture.json"
sha256sum data/runtime-target-bindings/v1/catalog.json "$workdir/capture.json"
```

The complete capture artifact includes a descriptive `source` field, so its whole-file SHA-256 is `6d73fd7c5ed726db33d4df026647a43b08479c32795705890ebc9ffe154e53f4`. The checked-in golden contains the captured fields consumed by the compatibility test: `rawCatalogSHA256`, `normalizedCatalogJSON`, `normalizedCatalogSHA256`, and `samples`. Verify the normalized JSON field itself against the recorded digest; do not compare the whole capture artifact hash to the checked-in fixture hash.
