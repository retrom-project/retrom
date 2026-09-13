# Deterministic 7z test fixtures

These archives contain only the freely distributable text files under
`payload/`. They exercise Retrom's container scanner and do not contain ROM,
BIOS, runtime, or other third-party payloads.

The fixtures are generated with 7-Zip 25.01 using disabled timestamps and
single-threaded compression. Run `generate.sh` from this directory and then
review the recorded SHA-256 values in this file before updating a fixture.
The encrypted fixture is pinned because the AES salt makes a newly generated
archive bytewise nondeterministic; normal generation preserves it. Set
`RETROM_REGENERATE_ENCRYPTED=1` only when intentionally replacing that fixture
and update its recorded digest in the same change.

| archive | bytes | SHA-256 |
| --- | ---: | --- |
| `single.7z` | 147 | `61740ac86bd28f3dfb7846b5b799c53b1c4d6c95f7952b2f3701304f3db9174e` |
| `ambiguous.7z` | 226 | `44d629706ce07245882d54cef7705404cb4d2298b11c75d8e677ccea58359a35` |
| `nested.7z` | 184 | `e8fcf678d21e4058b53f7c85d32311425aa7bc84b4825d9d164abc9559a71a84` |
| `encrypted.7z` | 222 | `6bc4f3520ec2e5c9dd7652d7d01dfb306751c8a279f2b7be38e92976f9e61fd0` |
| `casefold.7z` | 226 | `bc6fef851f8f79d24e1b6de6c6e1a1536123ef624959b241585910059af57aa1` |
| `symlink.7z` | 122 | `d9582bfc91162057f1518f1a60bfd3ab513931647902a711d248c3a23e05d038` |
| `unsupported-coder.7z` | 148 | `070574c07e8be53da3a5a1764c22c4efaee112fcb55cf7d08c4b73e7963a9525` |
