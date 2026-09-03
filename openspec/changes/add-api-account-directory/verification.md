# Verification

- `openspec validate add-api-account-directory --type change --strict --no-interactive` ✅
- `go test ./internal/directory ./internal/server/routes ./internal/config` ✅
- `make test-backend` ✅
- `make build` ✅
- `git diff --check` ✅

Notes:
- Directory uses the existing `*sql.DB`; no Migration, DB Role, credential, or dedicated pool was added.
- The implementation keeps `credentials` and `extra` out of the Directory pipeline and only projects bounded scalar fields.
- Added tests for unsupported methods, authorization header length limit, whitespace-only header length limit, bounded response encoding at the 4 MiB boundary, oversized response rejection, empty-directory contract, empty snapshots, ID ordering, invalid type invariant, 31/32-byte token bounds, token rotation, token bucket refill, static SQL safety, config token serialization redaction, and total-timeout expiry after encode.
- Pending: `5.6`, `6.3`, and `6.4` in `tasks.md`.
