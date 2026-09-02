## Purpose

为 Relay Station Control 提供完整、只读、最小披露的 Gateway API Account 候选目录，同时保持 Sub2API 原生 Account/Group 路由与运行时调度为唯一权威。

## ADDED Requirements

### Requirement: Directory endpoint and versioned envelope
Gateway SHALL 在 `GET /internal/v1/api-account-directory` 返回第一版 Directory JSON；成功响应顶层 SHALL 只包含 `schema_version`、`generated_at` 和 `accounts`，其中 `schema_version` 固定为字符串 `"1"`，`generated_at` 为 UTC RFC 3339 时间，`accounts` 为 JSON array。所有由 Directory route 产生的成功和失败响应 SHALL 包含 `Cache-Control: no-store`。

#### Scenario: Successful response shape
- **WHEN** 已授权的 `relay_control_reader` 调用精确 GET route 且 Gateway 能生成完整 Directory
- **THEN** Gateway 返回 HTTP 200，body 只包含 `schema_version="1"`、数据库 snapshot time `generated_at` 和 `accounts`

#### Scenario: Successful response disables storage
- **WHEN** Gateway 返回任一成功 Directory response
- **THEN** response 包含 `Cache-Control: no-store`

#### Scenario: Empty complete directory
- **WHEN** 一致性 snapshot 中不存在符合成员规则的 Account
- **THEN** Gateway 返回 HTTP 200 和空 `accounts: []`，且不得返回 null、404 或伪造条目

#### Scenario: Unsupported method
- **WHEN** caller 对该 path 使用 GET 以外的方法
- **THEN** Gateway 拒绝请求，且不得执行 Directory 查询或返回 Directory 数据

#### Scenario: Failed response disables storage
- **WHEN** Directory route 因 method、authentication、authorization、限流、超限、timeout 或内部错误返回 non-2xx
- **THEN** response 包含 `Cache-Control: no-store`

### Requirement: Fail-closed Account membership
Gateway SHALL 只包含 snapshot 中 `deleted_at IS NULL` 且 `type IN ('apikey', 'upstream')` 的 Account。`status`、schedulable、priority、rate limit、load、concurrency、cooldown、overload、breaker、近期流量和当前是否被选择 MUST NOT 改变成员资格。

#### Scenario: Approved Account types
- **WHEN** 未删除的 `apikey` 或 `upstream` Account 存在于 snapshot
- **THEN** 每个 Account 均出现在 Directory 中，不论其 persistent status 或 runtime scheduler state

#### Scenario: Deleted Account
- **WHEN** `apikey` 或 `upstream` Account 的 `deleted_at` 非 null
- **THEN** 该 Account 不出现在 Directory 中

#### Scenario: Known excluded Account types
- **WHEN** snapshot 包含 `oauth`、`setup-token`、`bedrock` 或 `service_account` Account
- **THEN** Gateway 排除这些 Account，且不得根据 name、platform、URL 或其他特征重新纳入

#### Scenario: Unknown future Account type
- **WHEN** snapshot 包含不在显式 allowlist 中的未来或未知 Account type
- **THEN** Gateway fail-closed 排除该 Account，而不是猜测其用途或使整个合法 snapshot 失败

### Requirement: Minimal Account projection
每个 `accounts` item SHALL 只包含 `id`、`name`、`platform`、`type`、`url` 和 `status`。`id` SHALL 是正整数且在响应内唯一；`name`、`platform`、`type` 和 `status` SHALL 是字符串；`url` SHALL 是 string 或 null。只有 `id` 是 binding identity，其他字段 MUST 仅作为识别和诊断上下文。

#### Scenario: Exact item fields
- **WHEN** Gateway 返回任一 Account item
- **THEN** item 恰好包含六个规定字段，不包含其他 Account、Group、routing 或 scheduler 字段

#### Scenario: Stable identity despite metadata change
- **WHEN** 同一 `accounts.id` 的 name、platform、url 或 status 在后续 snapshot 中变化
- **THEN** Directory 仍以原 `id` 表示同一 binding candidate，且不提供自动 rebind 或替代 identity

#### Scenario: Invalid required identity
- **WHEN** Gateway 无法为任一成员生成正整数且唯一的 `id`
- **THEN** Gateway 将整个请求作为 snapshot integrity failure 返回 non-2xx，不得跳过该成员

#### Scenario: Unknown platform and status strings
- **WHEN** approved Account 包含当前 Gateway 未识别但仍为 string 的未来 `platform` 或 `status`
- **THEN** Gateway 原样返回该 string，且不得仅因值未知而拒绝 snapshot

### Requirement: Stable ordering and complete snapshot
Gateway SHALL 在一次请求中返回全部成员，并严格按 `accounts.id ASC` 排序。HTTP 200 SHALL 表示响应完整、有效且未截断；Gateway MUST NOT 以 pagination、first-N、best-effort、partial data 或 success-shaped completeness flag 代替完整响应。

#### Scenario: Complete ordered response
- **WHEN** 成员总数和序列化结果均在 hard limits 内
- **THEN** Gateway 返回全部成员且相邻 item 的 `id` 严格递增

#### Scenario: Cannot determine full membership
- **WHEN** 数据库错误、并发 snapshot 建立失败或任何其他错误使 Gateway 无法确定完整成员集合
- **THEN** Gateway 返回明确 non-2xx，body 不包含任何 Account item

#### Scenario: Partial serialization failure
- **WHEN** 任一成员无法按契约序列化
- **THEN** Gateway 丢弃整个待发送结果并返回 non-2xx，不得发送已完成的前缀

### Requirement: Consistent source snapshot time
`accounts` 成员、字段值、排序与 `generated_at` SHALL 来自同一个 PostgreSQL MVCC statement snapshot 或等价的 READ ONLY REPEATABLE READ snapshot。`generated_at` SHALL 表示 Gateway database source snapshot time，不得使用 Control time、response completion time 或查询结束后的任意应用时钟。

#### Scenario: Concurrent Account mutation
- **WHEN** Account 在 Directory 读取期间被并发创建、更新、软删除或恢复
- **THEN** 成功响应只反映单一一致性 snapshot，不混合变更前后的成员或字段

#### Scenario: Empty snapshot source time
- **WHEN** 一致性 snapshot 的成员集合为空
- **THEN** Gateway 仍从同一个数据库 snapshot 返回有效 `generated_at`

### Requirement: Conservative URL sanitization
Gateway SHALL 仅从批准的 Account `credentials.base_url` scalar string 生成辅助 `url`，不得推导平台默认 URL、读取 proxy URL 或读取 `extra`。数据库 projection SHALL 在返回 scalar 前同时验证 JSON type 为 string 且 UTF-8 encoded value 不超过 4096 bytes；缺失、非 string 或超过 4096 bytes 时只向应用层返回 SQL NULL，不得读取超大 raw scalar。第一版输出 SHALL 只保留合法绝对 HTTP(S) URL 的小写 scheme、host 和显式 port，即 `scheme://host[:port]`；path、userinfo、query、fragment 和任何潜在嵌入式 Secret MUST 被移除。缺失、非字符串、超长、解析失败、非 HTTP(S)、缺少 host 或无法安全重建时 `url` SHALL 为 null，且 MUST NOT 回退原值。

#### Scenario: URL with path and sensitive components
- **WHEN** `credentials.base_url` 为带 userinfo、path、query 或 fragment 的合法 HTTP(S) URL
- **THEN** Gateway 只返回由 scheme、host 和显式 port 重建的 origin，不返回被移除组件

#### Scenario: Missing configured base URL
- **WHEN** approved Account 没有 `credentials.base_url`
- **THEN** Gateway 返回该 Account 且 `url` 为 null，不推导 provider 默认 endpoint

#### Scenario: Malformed or unsafe URL
- **WHEN** URL source 非字符串、格式错误、使用非 HTTP(S) scheme、缺少 host 或无法安全解析
- **THEN** Gateway 返回该 Account 且 `url` 为 null，不泄漏原始值，也不使完整 snapshot 失败

#### Scenario: Oversized URL source
- **WHEN** `credentials.base_url` string 的 UTF-8 encoded value 超过 4096 bytes
- **THEN** database projection 向应用层返回 NULL，Gateway 返回该 Account 且 `url` 为 null，超大 raw scalar 不进入应用内存、日志或错误

#### Scenario: IPv6 origin
- **WHEN** URL source 使用合法 IPv6 host 和显式 port
- **THEN** Gateway 返回保持有效方括号语法的 sanitized origin

### Requirement: Dedicated service authentication
Directory SHALL 只接受独立 `relay_control_reader` service token，并通过 `Authorization: Bearer <token>` 传递。Gateway runtime SHALL 解码 token 并验证 decoded length 至少为 32 bytes；Ops deployment SHALL 使用 CSPRNG 生成至少 256 bits random material。Token SHALL 与 Gateway admin JWT/session/cookie、普通用户或 Sub2API API Key、CLIProxyAPI Management Key、OAuth token 及其他应用 Secret 相互独立。

#### Scenario: Valid reader token
- **WHEN** caller 提交当前有效的独立 reader token
- **THEN** Gateway 将 caller 识别为 `relay_control_reader` 并继续执行 route 授权和资源准入

#### Scenario: Missing or invalid token
- **WHEN** token 缺失、格式错误、过期或不匹配
- **THEN** Gateway 返回统一 HTTP 401，且不得查询 Directory 或泄漏 token 配置状态

#### Scenario: Reused application credential
- **WHEN** caller 提交有效 admin JWT、admin API key、普通 API Key、OAuth token 或 CLIProxyAPI Management Key
- **THEN** Gateway 返回 HTTP 401，不将该凭据解释为 Directory reader token

#### Scenario: Runtime token too short
- **WHEN** 配置的 service token 可解码但 decoded length 少于 32 bytes
- **THEN** Gateway 拒绝启用 Directory，不以运行时熵估计替代长度校验

### Requirement: Exact authorization and management-network isolation
HTTP identity `relay_control_reader` SHALL 只被授权访问精确 GET Directory route。Gateway SHALL 负责 route、authentication、config hooks 和 default-disabled 行为；Ops SHALL 负责生产 TLS、restricted management network ACL、public-ingress deny 和 HTTP/DB Secret deployment。任一边界 MUST NOT 替代另一个，部署前置条件未满足时 Directory MUST 保持 disabled，且 MUST NOT 通过 public AI ingress 暴露。

#### Scenario: Neighboring internal route
- **WHEN** 已认证 reader identity 请求其他 internal、admin、Account、Group、API-key 或 credential route
- **THEN** Gateway 拒绝访问，且 reader identity 不获得任何隐式角色或额外权限

#### Scenario: Request outside management ingress
- **WHEN** 请求携带有效 reader token但来自 public AI ingress 或不满足 management network policy
- **THEN** 请求在到达 Directory handler 前被拒绝

#### Scenario: Plaintext transport
- **WHEN** caller 尝试通过不受信任网络上的非 TLS transport 访问 Directory
- **THEN** 部署入口拒绝或不路由该请求，不允许仅凭 token 成功

### Requirement: Least-privilege data access
Directory SHALL 使用独立 allowlist projection，只读取生成六个公开字段所需的列和经过 4096-byte ceiling 的 `credentials.base_url` scalar。Gateway 专用数据库 runtime role SHALL 为 `relay_directory_db_reader`；SECURITY DEFINER function owner SHALL 为 NOLOGIN `relay_directory_definer`。完整 `credentials`、完整 `extra` 和完整 Account DTO MUST NOT 进入 Directory response pipeline；`relay_directory_db_reader` MUST NOT 获得对 `accounts.credentials` 或其他业务表的通用读取权限。Control MUST NOT 直接连接 Gateway PostgreSQL，也 MUST NOT 获得任一 Gateway database credential。

#### Scenario: Projection execution
- **WHEN** Gateway 查询 Directory
- **THEN** 数据访问层只产生 approved scalar projection，不加载 credentials/extra JSON object、Account relations 或 scheduler state

#### Scenario: Reader database privilege probe
- **WHEN** 使用 `relay_directory_db_reader` 尝试直接读取 `accounts`、`credentials` 或非批准业务对象
- **THEN** PostgreSQL 拒绝访问，同时仍允许执行精确 Directory projection

#### Scenario: Definer cannot log in
- **WHEN** caller 尝试以 `relay_directory_definer` 建立数据库连接
- **THEN** PostgreSQL 拒绝登录，同时该 role 仍可作为 SECURITY DEFINER function owner

#### Scenario: Control receives no database credential
- **WHEN** Ops 为 Control 配置 Directory polling
- **THEN** Control 只获得 `relay_control_reader` HTTP token 和 management endpoint，不获得 `relay_directory_db_reader` 或 `relay_directory_definer` credential

### Requirement: No secret or internal-state disclosure
成功响应、错误响应、日志和指标 MUST NOT 包含 API Key plaintext、access/refresh/setup token、service token、完整 credentials/extra、proxy credential、raw URL、raw Account/upstream error、SQL、表名、数据库 hostname、stack trace、Group 或 routing relationship、priority、concurrency、schedulable、load、rate-limit/cooldown/overload/breaker、retry policy、scheduler score/state 或 model routing。

#### Scenario: Canary secrets in source data
- **WHEN** approved Account 的 credentials、extra、proxy、error 或 URL 非公开组件中包含唯一 canary secret
- **THEN** canary 不出现在成功 body、错误 body、日志或指标标签中

#### Scenario: Internal query failure
- **WHEN** PostgreSQL 返回包含 SQL、relation 或 hostname 的详细错误
- **THEN** client 只收到稳定的脱敏 error `code` 和固定公开 `message`，日志也不记录原始错误文本

### Requirement: Side-effect-free read path
Directory SHALL 是纯读取路径。它 MUST NOT 更新 Account 或 last-used/accessed 字段、刷新 credential、触发 scheduler outbox/cache/routing refresh、改变 schedulable/cooldown/breaker、使用 `FOR UPDATE` 或业务 advisory lock、推进 scheduler revision，或调用上游 AI/Relay Node。

#### Scenario: Successful read has no business side effects
- **WHEN** Directory 请求成功
- **THEN** Account、Group、scheduler/cache、outbox 和 upstream call counters 均保持不变，仅允许增加脱敏 access log 与 metrics

#### Scenario: Failed read has no business side effects
- **WHEN** Directory 请求因鉴权、限流、超时、数据库或序列化错误失败
- **THEN** 同样不产生任何 Account、routing、scheduler 或 upstream side effect

### Requirement: Fixed hard limits and whole-request failure
第一版 Directory SHALL 使用以下相互独立的 hard limits：最多 10,000 个 Account、最多 4 MiB 序列化 response body、2 秒数据库查询 timeout、authentication 成功后 3 秒 HTTP total request deadline、最多 2 个并发执行中的 Directory 请求。Gateway SHALL 在 authentication 成功后立即启动 total deadline；DB query deadline SHALL 为 query 开始时刻加 2 秒与剩余 total deadline 两者中的较早者。任一 hard limit 被触发时 SHALL 整体失败，不得截断、分页或返回部分结果。数据库可以用最多 10,001 行的 bounded sentinel query 检测 Account overflow。

#### Scenario: Account count limit
- **WHEN** 一致性 snapshot 包含超过 10,000 个成员
- **THEN** Gateway 通过第 10,001 个 sentinel member 检测 overflow，返回 HTTP 413 和 code `directory_account_limit_exceeded`，不返回 Account item 或 first-N success

#### Scenario: Response byte limit
- **WHEN** 完整 JSON body 将超过 4 MiB
- **THEN** Gateway 返回 HTTP 413 和 code `directory_response_limit_exceeded`，不发送被截断 body

#### Scenario: Database query timeout
- **WHEN** Directory projection 在 2 秒内未完成
- **THEN** Gateway 在不晚于剩余 total deadline 时取消查询，并返回 HTTP 503 和 code `directory_query_timeout` 或已先到期的 `directory_timeout`

#### Scenario: HTTP total timeout
- **WHEN** authentication 成功后的完整处理在 3 秒内未完成
- **THEN** Gateway 取消剩余工作并返回 HTTP 503 和 code `directory_timeout`，除非 response 已因 transport disconnect 不可写

#### Scenario: Concurrency bulkhead full
- **WHEN** 已有 2 个 Directory 请求正在执行
- **THEN** Gateway 不等待、不占用额外数据库连接，并返回 HTTP 429 和 code `directory_busy`

#### Scenario: Account limit below byte limit
- **WHEN** snapshot 包含 10,001 个短字段 Account 且完整编码结果小于 4 MiB
- **THEN** Gateway 仍因 Account count 独立 hard limit 返回 `directory_account_limit_exceeded`

#### Scenario: Byte limit below Account limit
- **WHEN** snapshot 不超过 10,000 个 Account 但完整编码结果超过 4 MiB
- **THEN** Gateway 仍因 response byte 独立 hard limit 返回 `directory_response_limit_exceeded`

#### Scenario: Both count and byte limits remain within bounds
- **WHEN** snapshot 不超过 10,000 个 Account 且完整编码结果不超过 4 MiB
- **THEN** Gateway 不因任一容量 limit 拒绝请求，并继续执行其余契约校验

### Requirement: Per-identity rate limit
Gateway SHALL 对 HTTP identity `relay_control_reader` 使用 non-blocking token bucket：refill rate 固定为 10 tokens/minute，capacity 固定为 2 tokens，每个请求 cost 固定为 1 token。Rate limiter 不可用或无法可靠判定时 SHALL fail-closed，不得绕过限制查询数据库；该定义不等价于 strict rolling-window “每分钟最多 10 次”。

#### Scenario: Normal polling
- **WHEN** Control 按 180 秒正常 polling 周期调用 Directory
- **THEN** 请求在未超过其他 hard limits 时通过 rate admission

#### Scenario: Retry storm
- **WHEN** reader identity 的 token bucket 可用 token 少于单请求 cost 1
- **THEN** Gateway non-blocking 返回 HTTP 429 和 code `directory_rate_limited`，不等待 refill 且不执行数据库查询

#### Scenario: Token bucket refill
- **WHEN** bucket 未满且时间经过
- **THEN** Gateway 按 10 tokens/minute 连续 refill，最多恢复到 capacity 2

#### Scenario: Rate decision unavailable
- **WHEN** rate limiter 无法可靠判定是否可接受请求
- **THEN** Gateway 返回 HTTP 503 或 429，不把故障当作允许

### Requirement: Stable failure contract
所有 Directory non-2xx 响应 SHALL 使用固定、脱敏的 JSON error envelope `{"code":"...","message":"..."}`，顶层 SHALL 只允许 string `code` 和 string `message` 两个字段。错误 body MUST NOT 包含 `accounts`、任何部分 Directory、metadata、details、reason、stack 或底层错误文本。相同 error code 的 `message` SHALL 是固定公开文案，不依赖底层 SQL、driver 或 runtime 错误文本。

#### Scenario: Exact error envelope
- **WHEN** Directory 返回任一 non-2xx response
- **THEN** JSON 顶层 key set 恰好为 `code` 和 `message`，并同时返回 `Cache-Control: no-store`

#### Scenario: Snapshot integrity failure
- **WHEN** required scalar 无效、ID 重复、顺序无法保证、序列化失败或完整性校验失败
- **THEN** Gateway 返回 HTTP 500 和 code `directory_snapshot_invalid`，不返回部分数据

#### Scenario: Database unavailable
- **WHEN** Directory 专用数据库连接或 projection 不可用且尚未超时
- **THEN** Gateway 返回 HTTP 503 和 code `directory_unavailable`

### Requirement: Data-plane priority and independence
Directory MUST NOT 成为任何 AI request path 的前置或依赖。CPU、内存、数据库连接和 goroutine 使用 SHALL 受专用预算约束；资源竞争、burst、retry storm 或 Control 停止 polling 时，Gateway SHALL 优先失败或 throttle Directory，而不是降低 AI 数据面可用性或改变 Sub2API 原生 routing/scheduling。

#### Scenario: Directory failure during AI traffic
- **WHEN** Directory 数据库、鉴权、限流或 handler 持续失败，同时 AI 请求进入 Gateway
- **THEN** AI 请求不等待 Directory、不读取其状态，并继续遵循原生 Account/Group routing 与 scheduling

#### Scenario: Resource pressure
- **WHEN** Gateway 处于数据库连接或 CPU 压力且 Directory 请求到达
- **THEN** Directory 受 bulkhead、timeout 或限流拒绝，不能耗尽为数据面保留的资源

#### Scenario: Control stops polling
- **WHEN** Control 长时间不再请求 Directory
- **THEN** Gateway 不修改 Account、Group、scheduler、routing 或 Relay Node 行为

### Requirement: First-version protocol exclusions
第一版 Directory SHALL 是无 pagination、无 cache 的普通 request/response GET。Gateway MUST NOT 返回或接受 cursor、page、limit、revision、hash、ETag/If-None-Match、delta/since、change stream、CDC、WebSocket、SSE、database LSN、scheduler revision、total count 或 `projection_complete`。

#### Scenario: Conditional or pagination input
- **WHEN** caller 提交 pagination、delta 或 conditional request 参数
- **THEN** Gateway 不改变 full-snapshot 语义，也不返回分页、304、增量或 success-shaped partial response

#### Scenario: Exact successful envelope
- **WHEN** Directory 请求成功
- **THEN** response 不包含任何第一版排除的 metadata 或协议字段
