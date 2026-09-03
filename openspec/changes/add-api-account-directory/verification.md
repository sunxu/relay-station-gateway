# Verification

- `openspec validate add-api-account-directory --type change --strict --no-interactive` ✅
- `go test ./internal/directory -run 'TestServiceServeRateLimits|TestServiceBurstDoesNotBlockUnrelatedRoute|TestServiceServeRejectsBusyRequests' -count=1` ✅
- host `127.0.0.1:18081` gateway port unreachable after removing the host publish ✅
- `go test ./internal/directory ./internal/server/routes ./internal/config` ✅
- `make test-backend` ✅
- `make build` ✅
- `git diff --check` ✅
- `./dev/devctl down` now succeeds without loading runtime secrets because `DIRECTORY_CURRENT_TOKEN=unused` is supplied to Compose during teardown ✅
- `./dev/devctl up` / `./dev/devctl check` passed with matching local Control/Gateway images; gateway host port 18081 stayed unreachable and `gateway-proxy` remained healthy ✅

Notes:
- Directory uses the existing `*sql.DB`; no Migration, DB Role, credential, or dedicated pool was added.
- The implementation keeps `credentials` and `extra` out of the Directory pipeline and only projects bounded scalar fields.
- Added tests for secret canary redaction, unsupported methods, authorization header length limit, whitespace-only header length limit, bounded response encoding at the 4 MiB boundary, oversized response rejection, empty-directory contract, empty snapshots, ID ordering, invalid type invariant, 31/32-byte token bounds, token rotation, token bucket refill, static SQL safety, config token serialization redaction, and total-timeout expiry after encode.
- Canary smoke verified the response body stayed redacted, gateway access logs did not contain the canary token, and Directory-specific metrics/log payloads are not emitted in this stack, so that part is N/A.
- Direct gateway smoke verified `POST` returns `{"code":"directory_method_not_allowed","message":"method not allowed"}` with `Cache-Control: no-store`, and missing auth returns `{"code":"directory_unauthorized","message":"Authorization header is required"}` with `Cache-Control: no-store`.
- That direct Gateway smoke is application-level evidence only; it does not satisfy ingress acceptance until the management/public proxy path is exercised end to end.
- `gateway-proxy` health stayed `running/healthy` after the ACL/config render change.
- Real ingress smoke via `gateway-proxy` verified management TLS + valid token returns Directory JSON with `Cache-Control: no-store`, missing/wrong token returns 401 with the fixed JSON envelope, unsupported method returns 405 with the fixed JSON envelope, and public ingress denies `/internal/v1/*`.
- The management ACL is rendered from the detected Compose subnet plus `127.0.0.1` for proxy self-healthchecks; the Gateway host port is no longer published on the host.
- Real shared-stack smoke also ran a live `POST /v1/responses` request through `gateway-proxy`; the AI request completed successfully while Directory stayed bounded by admission control.
- Management/public ingress behavior remained unchanged after the teardown placeholder fix.
- Ops deployment docs in `ops/dev/DEPLOYMENT.md` now state the default-disabled Directory, token generation, TLS/ACL, public-ingress deny, and Control credential boundary.
- `ops/dev/devctl check` ✅ (dev stack healthy).
- `TestServiceServeRateLimits`, `TestServiceBurstDoesNotBlockUnrelatedRoute`, and `TestServiceServeRejectsBusyRequests` cover the shared-pool/burst admission behavior.
