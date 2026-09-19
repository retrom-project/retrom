# Old Go probe observations

These frozen files preserve the actual pre-migration behavior of the three small engine detectors.
The capture imports the original packages at Retrom commit `85b2fac3bb3c423216949507b4123255d6b66077`.
Its raw output is retained byte-for-byte as `old-go-golden.json`; each owning Adapter keeps the
corresponding subset and records both complete and subset SHA-256 values in its provenance JSON.

For reproduction, use a separate temporary checkout of that commit, verify the original detector
source hashes against the adjacent text snapshots, copy `old-capture.go.txt` into an ignored
`.artifacts/refactor/small-engine-probes-capture.go` file there, and run it from that checkout's
module root with Go 1.26.5. Do not replace current production sources or regenerate expected output
from the new implementation. The captured bytes are public, generated inputs and contain no game
or BIOS payloads. Normal tests read only their committed engine subset and need no temporary
workspace reports.
