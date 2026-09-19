# Password PHC capture

The fixed PHC bytes in [password-golden.json](password-golden.json) were produced by the old Go implementation at revision `f39243478403695fbd95e68ac83da59f9e021c5b`, before the password execution moved from Capability to Adapter. The source program is preserved unchanged in [password-capture.go.txt](password-capture.go.txt).

- Toolchain actually executed: `go version go1.26.5 linux/amd64` with `GOTOOLCHAIN=local GOPROXY=off`.
- Old `internal/capability/security/authn/password.go` SHA-256: `2221f11564704b86cf5087ac0997b9c268004d7dc4efd2e6410022423ec681bb`.
- Capture program SHA-256: `4cf1c4a8a49dda2baaef48f6ca4ab79c2e87fd411c5f2a6a57816cd50c4b7062`.
- Golden SHA-256: `29f33cca2bc522bc9089163694532da1bbeb020057c39e09245500b4b1b52473`.
- Two fixed, public test passwords use the exact salt bytes `00 01 02 03 04 05 06 07 08 09 0a 0b 0c 0d 0e 0f`. These are test inputs, not application credentials.
- The capture also records five malformed PHCs with the old error text/identity and the production semaphore capacity of four.

To reproduce, export only `go.mod`, `go.sum` and the old `internal/capability/security/authn/{password,blocklist,principal}.go` files with `git archive` into a separate temporary module. Copy the saved capture program there as `internal/capability/security/authn/capture_test.go`. Set `RETROM_AUTHN_CAPTURE_OUTPUT` to a temporary output file, then run:

~~~sh
GOTOOLCHAIN=local GOPROXY=off go test -count=1 -run '^TestCaptureOldPasswordHasher$' ./internal/capability/security/authn
~~~

Use the pinned Go version and compare the resulting JSON bytes to the fixed golden. Do not regenerate the checked-in fixture from the new implementation.
