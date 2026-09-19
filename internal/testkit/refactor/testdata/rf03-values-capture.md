# RF03 frozen value capture

`rf03-values-82834ba.json` is the literal stdout of `rf03-value-capture.go.txt`
executed with Go 1.26.5 inside an isolated copy of commit
`82834bade1648da3067ebda1b1fc89c18577fd6a`. The current refactor implementation was
not used to generate expected values. The ten original implementation/module
files were byte-compared with `git show 82834ba:<path>` before capture, and their
SHA-256 values are recorded under `Provenance.Sources`. The fixed input and capture
program also have recorded SHA-256 values checked by the test.

To reproduce, use the isolated module at
`.artifacts/refactor/baseline-capture-82834ba`, with the checked-in capture text
copied exactly to its `internal/rf03valuecapture/main.go`, then run the recorded
command from that module:

```sh
GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly go run ./internal/rf03valuecapture ../../../internal/testkit/refactor/testdata/rf03-values-input.json
```

The Go binary must be the repository's pinned Go 1.26.5. Compare stdout to the
checked-in golden; do not overwrite the golden when the new implementation fails.
The capture creates and removes a temporary CAS directory within its own ignored
capture-program directory. It does not alter original production sources, child
repositories, the root baseline, migrations, or the formal RF01 golden.

The fixtures exercise distinct boundaries:

- HTTP Candidate JSON comes from the old Hasheous normalizer. Its complete input
  body, including unknown nested fields, Unicode and surrounding whitespace, is
  retained separately as base64 and compared to both the input and new raw bytes.
- Sent HTTP body, method, URL and both provider/request-codec digests are captured.
  Null Candidate members and nonnil empty objects/arrays remain distinct.
- Two real `jobs.Snapshot` values cover null/non-null error codes, Unicode/HTML
  escaping and an int64 beyond the JavaScript exact-integer range.
- Three real `saves.ManualResult` values use its old custom `MarshalJSON`, covering
  a null screenshot URL, Unicode/HTML escaping, and the preview branch's omission
  of product-only fields. The type has conditional omission rather than literal
  `omitempty` tags; no such tag is invented by this fixture.
- The old Blobstore actually hashes and publishes a fixed 40-byte body. The
  captured JSON is only its four hashes and size; the execution-specific Path and
  Existing fields are deliberately excluded from the immutable fact projection.
  The new Adapter must return a `model/blob.PreparedBlob` whose JSON matches it.
- Netplay Protocol and Manifest use the separately captured existing
  `internal/model/netplayprofile/testdata/registry-golden.json` fixture and the
  pinned manifest's raw SHA-256. That existing golden is not rewritten here.

The test uses temporary files only for the Blobstore value comparison. The
separate `TestRefactorRF03_minimal_fake` continues to use only in-memory Model
ports and performs no database, file or HTTP operations.
