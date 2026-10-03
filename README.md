# Retrom v0.0.105 acceptance findings

2026-10-04. Real deployed Retrom v0.0.105, Runtime 0.58.6, Chromium.

- Password confirmation mismatch: natural HTTP 422; generic toast and missing invalid-field association. Password was not changed. Screenshot masks password fields and account facts.
- Parent attachment: real upload, validation and dependency state. A Parent archive without BIOS is rejected even though the BIOS node is SATISFIED_EXTERNAL. A control archive adding the same six installed BIOS files succeeds. No ROM or BIOS bytes are published.
- Performance: controlled bulk reimport and BIOS scan, frontend/API latency and matching server stage timings. This is a sampled workload, not an all-endpoint SLA claim.

Deployment addresses, credentials and raw account/session responses are excluded. Full acceptance coverage is kept in the local output report.
