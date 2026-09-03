# Verification

- `openspec validate add-api-account-directory --type change --strict --no-interactive` ✅
- `go test ./internal/directory -run 'TestServiceServeRateLimits|TestServiceBurstDoesNotBlockUnrelatedRoute|TestServiceServeRejectsBusyRequests' -count=1` ✅
- `python3` direct gateway smoke on `127.0.0.1:18081` ✅
- `go test ./internal/directory ./internal/server/routes ./internal/config` ✅
- `make test-backend` ✅
- `make build` ✅
- `git diff --check` ✅

Notes:
- Directory uses the existing `*sql.DB`; no Migration, DB Role, credential, or dedicated pool was added.
- The implementation keeps `credentials` and `extra` out of the Directory pipeline and only projects bounded scalar fields.
- Added tests for secret canary redaction, unsupported methods, authorization header length limit, whitespace-only header length limit, bounded response encoding at the 4 MiB boundary, oversized response rejection, empty-directory contract, empty snapshots, ID ordering, invalid type invariant, 31/32-byte token bounds, token rotation, token bucket refill, static SQL safety, config token serialization redaction, and total-timeout expiry after encode.
- Canary smoke verified the response body stayed redacted, gateway access logs did not contain the canary token, and Directory-specific metrics/log payloads are not emitted in this stack, so that part is N/A.
- Direct gateway smoke verified `POST` returns `{"code":"directory_method_not_allowed","message":"method not allowed"}` with `Cache-Control: no-store`, and missing auth returns `{"code":"directory_unauthorized","message":"Authorization header is required"}` with `Cache-Control: no-store`.
- Ops deployment docs in `ops/dev/DEPLOYMENT.md` now state the default-disabled Directory, token generation, TLS/ACL, public-ingress deny, and Control credential boundary.
- `ops/dev/devctl check` ✅ (dev stack healthy).
- `TestServiceServeRateLimits`, `TestServiceBurstDoesNotBlockUnrelatedRoute`, and `TestServiceServeRejectsBusyRequests` cover the shared-pool/burst admission behavior.
