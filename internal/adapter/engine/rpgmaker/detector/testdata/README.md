# RPG Maker detector compatibility capture

The unchanged old-golden.json is the output of the pre-split Go implementation,
identified by the commit, Go version and per-source SHA256 in provenance.json.
It contains 122 observations, including exact Profile JSON, typed error fields
and cause checks, resource counts and real input limits. Its SHA256 is locked by
TestOldDetectorGoldenAndInputsKeepCapturedHashes.

fixture-inputs.json records the actual original synthetic bytes and relative
paths of the existing public fixture trees. bounded-inputs.json records the
original format limits and synthetic stream lengths; large streams are produced
without allocating an input file tree. Public fixtures retain their existing
generation sources and licenses. The MZ fixture is the project-owned shape and
security fixture, not a claim of licensed MZ runtime acceptance.

old-capture.go.txt is the same-package capture test. To reproduce the old run,
restore the source/test files from the recorded commit and verify their hashes,
add the capture test using a Go overlay, and set RF04_REPOSITORY_ROOT and
RF04_CAPTURE_DIR to explicit checkout/output directories. The input capture
used all recorded original source/test files restored through an overlay and
hid the new Inspection file. All 122 observations matched the frozen golden
before the input files were saved.

Production code never reads these files or a temporary design directory.
Replay tests exercise the real Adapter and pure Inspection without retaining an
old implementation or a compatibility Detect API. Private catalog.read cases
use real selected probes and the Adapter reader, with size decisions made by
the same pure Probe rule used by Inspection.

Raw map visitation and OS read chunk sizes are observations, not new public
ordering guarantees. These captured cases have one decisive failure or a stable
complete result; the original public assertions and all mismatches remain enabled.
