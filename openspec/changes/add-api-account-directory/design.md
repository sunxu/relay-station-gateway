## Context

参见 `proposal.md` 的动机。本设计受 System Design v1.8 / R4.7 §9.7、§20–24 与 ADR-0002 §2.2–2.5 约束：Gateway 是一个 Sub2API deployment，Sub2API 原生 Account/Group 与 runtime scheduler 仍是唯一真相，Directory 只向 Control 提供 binding candidate snapshot。

当前实现事实：

- `backend/ent/schema/account.go` 将 `accounts` 定义为带 `SoftDeleteMixin` 的实体；`id/name/platform/type/credentials/status/deleted_at` 均已有事实来源。
- `backend/internal/domain/constants.go` 已冻结实际 type value：`apikey`、`upstream`、`oauth`、`setup-token`、`bedrock`、`service_account`。
- 可配置 upstream locator 当前存于 `accounts.credentials ->> 'base_url'`；完整 `credentials` 同时包含 API key/token，不能通过完整 `service.Account` DTO 读取后再 redaction。
- Gateway 已有共享 `*sql.DB` 和 PostgreSQL identity。Directory 不改变数据库 schema、访问身份、credential 或连接配置。
- HTTP 使用 Gin 集中注册 route；现有 admin/JWT/API-key middleware 都代表不同 trust domain，不能复用。
- 当前环境只有一个 Gateway 实例；本设计不为未来多 Gateway 或水平扩容预建协调层。

## Goals / Non-Goals

**Goals:**

- 用最小独立组件和一条 raw SQL 完成严格 whole-response Directory，不扩展通用 Account repository interface。
- 只让 approved scalar 进入应用层，不 materialize 完整 credentials、extra、Account entity 或关系。
- 用固定 bulkhead、timeout、size 与 token-bucket budgets 约束共享数据库资源使用。
- 使成功和所有失败路径都无法触达 Account mutation、scheduler/cache/outbox 或 upstream client。
- 给后续 Sub2API upstream sync 留下小而显式的 route/module 接线和回归门禁。

**Non-Goals:**

- 不实现 Control ingestion、binding、resolution、duplicate ownership 或 topology UI。
- 不修改 Account/Group schema 语义，不增加 Relay identity 字段，不推断 Account 是否实际指向 CLIProxyAPI。
- 不新增任何数据库变更脚本、schema object、访问身份、credential 或 connection pool。
- 不抽象通用 internal-service framework、通用 projection DSL 或第二套路由/调度层。
- 不提供管理 UI、管理 CRUD、Group export、cache、pagination、incremental protocol 或 mTLS。

## Decisions

### 1. 使用独立垂直模块和精确 route

新增小型 `backend/internal/directory/` 模块，内部包含 auth、raw SQL repository、URL sanitizer、service、handler 和测试；route 文件只在 `/internal/v1/api-account-directory` 注册 GET，并通过现有 server composition 注入。成功响应直接写 Directory envelope，`schema_version` 使用 Go integer 字段并编码为 JSON number `1`，不套用面板 `response.Response`；失败响应使用该模块自己的固定 `{code,message}` allowlist envelope。

不把方法加入庞大的 `service.AccountRepository`：现有接口被大量 scheduler、admin 和测试 stub 实现，扩展会放大无关修改面，而且完整 Account DTO 含 Secret。

**备选方案：复用 `/api/v1/admin/accounts`。** 放弃，因为它接受 admin trust domain、分页且加载远多于六个字段。

### 2. 复用现有连接执行单条最小 raw SQL

Directory repository 注入 Gateway 已有 `*sql.DB`，只执行一条固定 raw SQL：

```sql
WITH snapshot AS (
  SELECT transaction_timestamp() AS generated_at
),
directory AS (
  SELECT
    id,
    name,
    platform,
    type,
    CASE
      WHEN jsonb_typeof(credentials -> 'base_url') = 'string'
      THEN CASE
        WHEN octet_length(credentials ->> 'base_url') <= 4096
        THEN credentials ->> 'base_url'
        ELSE NULL
      END
      ELSE NULL
    END AS url_source,
    status
  FROM accounts
  WHERE deleted_at IS NULL
    AND type IN ('apikey', 'upstream')
  ORDER BY id ASC
  LIMIT 10001
)
SELECT snapshot.generated_at, directory.*
FROM snapshot
LEFT JOIN directory ON TRUE
ORDER BY directory.id ASC
```

该 statement 在 SQL projection 内完成 membership、排序、overflow sentinel、source time 和 URL scalar ceiling。LEFT JOIN sentinel 让空目录仍返回 database `generated_at`。扫描到第 10,001 个成员时丢弃全部结果并返回 413；`LIMIT 10001` 不是 pagination 或 first-N success。

Gateway 现有 DB identity 本身已有业务权限。Directory 最小披露不伪装成新的 PostgreSQL privilege boundary，而由以下三项共同保证：

1. 固定 raw SQL 只选择 approved scalar。
2. Directory 模块不依赖完整 Account repository/DTO、Ent Account entity、scheduler 或 mutation service。
3. 静态 SQL、sqlmock、canary 和依赖测试阻止完整 credentials、写 SQL 或副作用进入该路径。

Control 仍绝对禁止直连 Gateway PostgreSQL，也不获得 Gateway DB credential。

**备选方案：增加数据库隔离层。** 放弃，因为第一版单 endpoint 的安全收益不足以抵消数据库对象、部署 Secret 和连接预算复杂度；应用层 allowlist projection 已覆盖最小披露要求。

**备选方案：复用 Ent Account query。** 放弃，因为 Ent 会 materialize 完整 entity，增加 credentials/extra 进入内存的风险。

### 3. 同一 statement snapshot 与 bounded scan

CTE 与外层 SELECT 属于一个 PostgreSQL statement snapshot。Repository 不开启额外 transaction，不使用 `FOR UPDATE` 或 advisory lock。扫描时 nullable `id` 只表示空集合 sentinel；非空行必须通过正 ID、唯一、严格递增和 required scalar 检查。未知但合法 string 的 platform/status 原样保留。

**备选方案：先查时间再查 rows。** 放弃，因为需要显式 REPEATABLE READ transaction，增加共享连接占用和错误路径。

### 4. URL 第一版只输出 sanitized origin

数据库只向应用返回不超过 4096 bytes 的 string scalar 或 NULL。应用收到有界 `url_source` 后立即用 Go `net/url` 解析，只接受 absolute `http`/`https` 且 host 非空；使用 scheme、`Hostname()` 和合法显式 `Port()` 重建 origin，scheme/hostname 小写，IPv6 重新加方括号。永远丢弃 userinfo、path、query 和 fragment；缺失、非 string、超长、解析或重建失败均返回 null。

没有配置 `base_url` 时不调用 `Account.GetBaseURL()` 推断 provider default。Raw source 只存在于单行 scan local variable，不写日志、error、metric 或 response DTO，并在转换后不保留。

**备选方案：保留“看起来安全”的 path。** 放弃，因为无法穷举嵌入 path 的 API key、tenant secret 或签名格式。

### 5. 独立 service-token authentication

Ops 使用 CSPRNG 生成至少 32 random bytes，再以 RFC 4648 unpadded Base64URL 编码。HTTP wire format 固定为 `Authorization: Bearer <base64url-token>`；middleware 严格限制 header 大小和单一 Bearer scheme。

Gateway 在启动/reload 时解码 configured current 和可选 previous token；任一 configured token malformed 或 decoded length 少于 32 bytes 时拒绝启用 Directory。每个请求同样先严格 decode bearer token并检查 decoded length，malformed/short token 统一 401；通过格式检查后，对 decoded bytes 做 constant-time comparison。Current/previous 使用相同格式和校验；previous 只在显式 rotation window 内有效。

配置默认 disabled。Gateway 负责 route、authentication、config hooks 和 runtime token validation；Ops 负责生产 TLS、management source ACL、public-ingress deny、token 生成和 Secret deployment。Ops 前置条件未验证完成前不得设置 enabled。

### 6. Admission 顺序和固定资源预算

处理顺序固定为：

```text
management ingress/TLS
→ method/path routing
→ header shape + token auth
→ start 3s total request deadline
→ per-identity token-bucket admission
→ non-blocking concurrency semaphore
→ shared DB query with deadline=min(now+2s, total deadline)
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
| Identity rate | token bucket: refill 10/min, capacity 2, cost 1 | 429 `directory_rate_limited` |

3 秒 total deadline 在 authentication 成功后立即开始。DB query deadline 取 `min(queryStart+2s,totalDeadline)`。JSON 先编码到带 4 MiB ceiling 的内存 buffer，验证完成后才设置 200/write body。10,000 Account 与 4 MiB 是独立 limits。

Token bucket 进程内、单 identity、non-blocking、fail-closed，不依赖 Redis。并发 semaphore 在查询前立即拒绝第三个执行中请求；共享数据库连接的占用最多来自两个 Directory 请求，并受 2 秒 query timeout 约束。若共享 pool 已无可用连接，Directory 等待也受 query/total deadline 限制并优先失败。

所有 Directory response 在最外层 middleware 设置 `Cache-Control: no-store`。Non-2xx 只编码两个 string 字段 `{"code":"...","message":"..."}`，不附加 metadata、reason、details、partial Directory 或底层错误。

### 7. Side-effect isolation by construction

Directory module 只依赖现有 `*sql.DB` 的窄 query interface、timeout、sanitizer 和 response writer，不注入 Account service、Account repository、Ent Account client、scheduler cache、Redis、outbox、Group repository 或 upstream clients。唯一 SQL statement 是受测试冻结的 SELECT，不包含 DML、DDL、row lock 或 advisory lock。

因此成功、错误、timeout 或 Control 停止 polling 都没有可调用的 Account/scheduler mutation path。静态 SQL test 证明无写语句；dependency test 证明无数据面服务依赖；canary test 证明完整 credentials/raw URL 不进入 result、body 或日志。

### 8. Stable redacted observability

日志/指标 allowlist：

```text
request_id, identity=relay_control_reader, result,
latency_ms, row_count, response_bytes, failure_class
```

禁止把 query、driver error、URL source、Account name、platform/status cardinality、token 信息或数据库 endpoint 放入日志/metric label。内部错误映射到与 HTTP `code` 同词典的固定 `failure_class`；原始数据库错误不格式化输出。

### 9. Compatibility and upstream synchronization

该 route 不挂到 `/api/v1/admin` 或公开 gateway route，不修改现有 Account/Group handler/service/repository interface，也不参与任何 AI request handler dependency。数据库 schema、访问身份、credential 和连接配置保持不变。

后续同步 Sub2API upstream 时，将独立 module、单行 route registration、config hook 和 contract tests 作为保留面；如果 upstream Account schema/type constants/base URL source变化，先更新本 OpenSpec 和固定 raw SQL，不在 runtime 猜测兼容。

## Risks / Trade-offs

- **[Gateway DB identity 权限大于 Directory 所需]** → 接受现有事实；用固定 SQL allowlist、无完整 DTO 依赖、静态查询检查和 Secret canary tests 缩小应用路径，而不是新增数据库权限体系。
- **[共享 DB pool 与数据面竞争]** → token bucket、并发 2、query 2s、total 3s 形成硬边界；压力下 Directory 超时/拒绝，不增加额外连接预算。
- **[10,000 Account 或 4 MiB 上限未来不足]** → 整体 413；通过新 OpenSpec 调整，禁止临时截断。
- **[Raw `base_url` scalar 在应用内短暂存在]** → SQL 先执行 type 与 4096-byte ceiling，只返回有界 scalar，应用立即 origin-only sanitize。
- **[进程内 rate limit 在未来多实例下不是全局限制]** → 当前单 Gateway 决策下接受；拓扑变化必须新架构评审。
- **[同 listener 误暴露 internal route]** → Ops reverse proxy/firewall 显式 deny public ingress；service auth 仍独立 fail-closed。

## Rollout Plan

本 change 没有数据库变更。实施与发布顺序：

1. 实现并验证固定 raw SQL projection、模块依赖隔离、auth、limits 和 route，保持 Directory disabled。
2. Ops 使用 CSPRNG 生成至少 32 random bytes，以 unpadded Base64URL 编码 current/previous token，并配置生产 TLS、management ACL 与 public-ingress deny。
3. 部署后先在 disabled 状态运行回归与数据面压力检查。
4. 从 management network 完成认证、空/小/容量边界 smoke tests 后再启用 Directory。
5. 观察 latency、busy/rate/timeout、row count 和 response bytes；异常时先停用 Directory。

回滚只需停止 Control polling、停用 route/token 并回退应用代码；没有 Directory 数据库对象、credential 或数据变更需要清理。
