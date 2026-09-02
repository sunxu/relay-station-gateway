## Context

参见 `proposal.md` 的动机。本设计受 System Design v1.8 / R4.7 §9.7、§20–24 与 ADR-0002 §2.2–2.5 约束：Gateway 是一个 Sub2API deployment，Sub2API 原生 Account/Group 与 runtime scheduler 仍是唯一真相，Directory 只向 Control 提供 binding candidate snapshot。

当前实现事实：

- `backend/ent/schema/account.go` 将 `accounts` 定义为带 `SoftDeleteMixin` 的实体；`id/name/platform/type/credentials/status/deleted_at` 均已有事实来源。
- `backend/internal/domain/constants.go` 已冻结实际 type value：`apikey`、`upstream`、`oauth`、`setup-token`、`bedrock`、`service_account`。
- 可配置 upstream locator 当前存于 `accounts.credentials ->> 'base_url'`；完整 `credentials` 同时包含 API key/token，不能通过现有完整 `service.Account` DTO 读取后再 redaction。
- HTTP 使用 Gin 集中注册 route；现有 admin/JWT/API-key middleware 都代表不同 trust domain，不能复用。
- 当前主数据库 pool 服务整个应用。Directory 若共用它，retry storm 可能与 AI 数据面争抢连接，因此需要独立、小容量、只读 pool 和数据库身份。
- 当前环境只有一个 Gateway 实例；本设计不为未来多 Gateway 或水平扩容预建协调层。

## Goals / Non-Goals

**Goals:**

- 用最小独立组件完成严格 whole-response Directory，不扩展通用 Account repository interface。
- 从数据库权限到 HTTP 输出保持 allowlist projection，避免 credentials/extra object 进入应用 pipeline。
- 用固定 bulkhead、timeout、size 与 rate budgets 让控制面读取可被快速拒绝。
- 使成功和所有失败路径都无法触达 Account mutation、scheduler/cache/outbox 或 upstream client。
- 给后续 Sub2API upstream sync 留下小而显式的 route/module 接线和回归门禁。

**Non-Goals:**

- 不实现 Control ingestion、binding、resolution、duplicate ownership 或 topology UI。
- 不修改 Account/Group schema 语义，不增加 Relay identity 字段，不推断 Account 是否实际指向 CLIProxyAPI。
- 不抽象通用 internal-service framework、通用 projection DSL 或第二套路由/调度层。
- 不提供管理 UI、管理 CRUD、Group export、cache、pagination、incremental protocol 或 mTLS。

## Decisions

### 1. 使用独立垂直模块和精确 route

新增小型 `backend/internal/directory/` 模块，内部包含 config、auth middleware、projection repository、URL sanitizer、service、handler 和测试；route 文件只在 `/internal/v1/api-account-directory` 注册 GET，并通过现有 server composition 注入。成功响应直接写 Directory envelope，不套用面板 `response.Response`，否则顶层会多出 `code/message/data` 并违反契约；失败响应使用该模块自己的固定 `{code, message}` allowlist envelope。

不把方法加入庞大的 `service.AccountRepository`：现有接口被大量 scheduler、admin 和测试 stub 实现，扩展会放大无关修改面，而且完整 Account DTO 含 Secret。

**备选方案：复用 `/api/v1/admin/accounts`。** 放弃，因为它接受 admin trust domain、分页且加载远多于六个字段。

### 2. 数据库 SECURITY DEFINER projection 加专用 pool

通过不可变 SQL migration 创建专用 database role `relay_control_reader` 和单一 `SECURITY DEFINER` set-returning function。Function 固定：

```sql
WHERE deleted_at IS NULL
  AND type IN ('apikey', 'upstream')
ORDER BY id ASC
```

并只返回：

```text
id, name, platform, type, credentials ->> 'base_url' AS url_source, status
```

Function owner 只拥有实现该 projection 所需权限，固定 `search_path`，所有对象 schema-qualified；撤销 public execute。运行 role 只有该 function 的 execute 权限，没有 `accounts`、`credentials`、`extra` 或其他表的直接 SELECT 权限。

Gateway 新增专用数据库配置和 `sql.DB`：`MaxOpenConns=2`、`MaxIdleConns=0`、连接 lifetime 有界。Token Secret 与数据库 password 由部署 Secret/env 注入，禁止存入 settings、日志或 API。应用启动时若 Directory enabled 但 Secret、TLS/network deployment prerequisite 或专用数据库配置缺失，则拒绝启用该 route，而不是回退主 pool。

**备选方案：复用 Ent Account query。** 放弃，因为 Ent materializes 完整 entity，并使用拥有通用表权限的主 pool。

**备选方案：创建 view。** 放弃，因为调用者仍需获得 view SELECT，owner/permission/search-path 管理更容易漂移；固定 function 提供更窄的 callable surface。

### 3. 单 SQL statement 建立完整 snapshot

Repository 用一个 statement 同时取得 database time、完整成员与顺序：

```sql
WITH snapshot AS (
  SELECT transaction_timestamp() AS generated_at
),
directory AS (
  SELECT * FROM relay_control_api_account_directory()
)
SELECT snapshot.generated_at, directory.*
FROM snapshot
LEFT JOIN directory ON TRUE
ORDER BY directory.id ASC
```

LEFT JOIN sentinel 让空目录也返回数据库 `generated_at`。扫描时将 nullable `id` 解释为空集合标记；非空行必须通过正 ID、唯一、严格递增和 required scalar 检查。SQL context deadline 为 2 秒。Function 和外层 statement 共享同一 PostgreSQL statement snapshot；不需要 transaction、`FOR UPDATE` 或 advisory lock。

**备选方案：先查时间再查 rows。** 放弃，因为需要显式 REPEATABLE READ transaction，增加连接占用和错误路径。

### 4. URL 第一版只输出 sanitized origin

Projection DTO 的 `url_source` 是唯一允许短暂持有的敏感 scalar，读取后立即用 Go `net/url` 解析。只接受 absolute `http`/`https` 且 host 非空；使用 scheme、`Hostname()` 和合法显式 `Port()` 重建 origin，scheme/hostname 小写，IPv6 重新加方括号。永远丢弃 userinfo、path、query 和 fragment；解析或重建失败返回 null。

这比尝试识别 path 中所有 token pattern 更保守，也满足 `scheme://host[:port][/path]` 中 path 可选的契约。没有配置 `base_url` 时不调用 `Account.GetBaseURL()` 推断 provider default，因为 Directory 应展示持久化 locator，而不是 runtime-derived routing。

Raw source 只存在于单行 scan local variable，不写日志、error、metric 或 response DTO，并在转换后不保留。

**备选方案：保留“看起来安全”的 path。** 放弃，因为无法穷举嵌入 path 的 API key、tenant secret 或签名格式。

### 5. 独立 service-token authentication

使用 `Authorization: Bearer`，严格限制 header 大小和单一 scheme；token 解码后要求至少 32 个随机 bytes，并以 constant-time comparison 校验当前 Secret。Middleware 只设置不可伪造的内部 identity `relay_control_reader`，不设置 user/admin role，也不调用 admin/JWT/API-key service。

配置默认 disabled。启用时必须存在有效 token Secret；轮换采用部署 Secret 同时配置 current 与短期 previous token，previous 只在显式 rotation window 内有效，窗口结束后移除并 reload/restart。日志只记录固定 identity label，绝不记录 header、token hash 或 prefix。

TLS termination 和 management source ACL 由 deployment reverse proxy/firewall 强制；public AI ingress 不路由 `/internal/v1/*`。应用层 route auth 是第三道独立边界，不因为网络或 TLS 存在而省略。

**备选方案：复用 admin API key/JWT。** 放弃，因为其权限远大于只读 route，且会耦合交互用户/session 生命周期。

### 6. Admission 顺序和固定资源预算

处理顺序固定为：

```text
management ingress/TLS
→ method/path routing
→ header shape + token auth
→ per-identity rate admission
→ non-blocking concurrency semaphore
→ 3s total context
→ dedicated DB query with 2s deadline
→ validate + sanitize + encode into bounded buffer
→ single response write
```

第一版 budgets：

| Budget | Value | Failure |
|---|---:|---|
| Account count | 10,000 | 413 `directory_account_limit_exceeded` |
| Encoded body | 4 MiB | 413 `directory_response_limit_exceeded` |
| DB query | 2s | 503 `directory_query_timeout` |
| HTTP total | 3s | 503 `directory_timeout` |
| Concurrent executions | 2 | 429 `directory_busy` |
| Identity rate | 10/min, burst 2 | 429 `directory_rate_limited` |

Count limit 通过扫描第 10,001 行判定，不使用 SQL `LIMIT 10001` 作为返回截断；一旦发现超限就整体失败。JSON 先编码到带 4 MiB ceiling 的内存 buffer，验证完成后才设置 200/write body，避免流式 partial success。

Rate limiter 使用进程内、单 identity 的小型 token bucket；当前架构只有一个 Gateway，没必要引入 Redis failure mode。它 fail-closed，且在 semaphore/DB 之前执行。未来多 Gateway 会改变全局 rate semantics，必须另行评审，不能静默沿用本实现。

### 7. Side-effect isolation by construction

Directory module 只依赖专用 query interface、clock/timeout、sanitizer 和 response writer，不注入 Account service、scheduler cache、Redis、outbox、Group repository 或 upstream clients。SQL role 只有 projection execute 权限，statement 为 SELECT 且 function 标记只读稳定性，不存在 mutation capability。

因此 Directory 的成功、错误、timeout、panic recovery 或 Control 停止 polling 都没有可调用的 Account/scheduler mutation path。测试用 mutation/outbox/upstream counters 与 SQL privilege probes证明此边界；不通过“约定不要调用”来保证。

### 8. Stable redacted observability

成功日志/指标 allowlist：

```text
request_id, identity=relay_control_reader, result,
latency_ms, row_count, response_bytes, failure_class
```

禁止把 query、driver error、URL source、Account name、platform/status cardinality、token 信息或数据库 endpoint 放入日志/metric label。内部错误先映射到固定 classification，再记录 classification；原始数据库错误只在内存中用于 `errors.Is` 分类，不格式化输出。

指标至少覆盖 request result、latency、busy/rate/timeout、row count 与 response bytes。正常 access logging 必须依赖既有 header redaction，并增加 canary test 防止 Authorization 泄漏。

### 9. Compatibility and upstream synchronization

该 route 不挂到 `/api/v1/admin` 或公开 gateway route，不修改现有 handler/service/repository interface，也不参与 Wire 中任何 AI request handler dependency。Migration 只增加专用 role/function，不改 `accounts` columns、indexes 或 triggers。

后续同步 Sub2API upstream 时，将独立 module、单行 route registration、config provider 和 contract tests 作为保留面；如果 upstream Account schema/type constants/base URL source变化，先更新本 OpenSpec 和 projection migration，不在 runtime 猜测兼容。

## Risks / Trade-offs

- **[10,000 Account 或 4 MiB 上限未来不足]** → 整体 413 并告警；先做容量测量，再通过新 OpenSpec 调整或引入新协议，禁止临时截断。
- **[SECURITY DEFINER function 配置错误扩大权限]** → 固定 owner/search_path、schema-qualified object、撤销 PUBLIC、数据库 privilege regression test 和 migration review。
- **[Raw `base_url` scalar 在应用内短暂存在]** → 独立 projection 只返回该 scalar，立即 origin-only sanitize，canary 测试覆盖 body/log/error；不加载 JSON object。
- **[进程内 rate limit 在未来多实例下不是全局限制]** → 当前单 Gateway 决策下接受；拓扑变化必须新架构评审。
- **[Origin-only URL 降低管理员识别度]** → 安全优先保留 host/port；只有证明 path 必需且能建立严格 Secret policy 后才扩展。
- **[同 listener 误暴露 internal route]** → reverse proxy/firewall 显式 deny public ingress，加部署 smoke test；service auth 仍独立 fail-closed。
- **[专用 DB pool 增加部署配置]** → 默认 disabled、启动配置校验、最多两连接；不回退高权限主 pool。

## Migration Plan

1. 合入 SQL migration，创建/校验专用 role、function、owner、grant/revoke；先运行 privilege probes，不启用 HTTP route。
2. 生成独立 256-bit 以上 token 和数据库 Secret，配置 management TLS ingress 与 source ACL，保持 Directory disabled。
3. 部署 Gateway module，运行 contract、secret-negative、并发 mutation、hard-limit 与 data-plane isolation tests。
4. 启用 Directory，先从 management network 执行空/小/容量边界 smoke tests，再让 Control 以 180 秒周期 polling。
5. 观察 latency、busy/rate/timeout、row count 和 response bytes；任何异常先停用 Directory route，不修改 Account 或 scheduler。

回滚顺序为：停止 Control polling → 从 ingress 撤销 route → disable Gateway Directory/revoke token → 回滚应用。专用 function/role 可在确认无 caller 后由后续 migration 删除；即使暂时保留也没有业务写权限。整个回滚不需要恢复 Account、Group、binding 或 scheduler state。
