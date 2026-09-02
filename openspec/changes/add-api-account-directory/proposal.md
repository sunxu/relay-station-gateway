## Why

Phase 4 需要让 Control 在不直连 Gateway PostgreSQL、也不复制 Sub2API 调度真相的前提下，取得可供管理员建立 Relay Node 与 Gateway Account binding 的候选目录。当前 Gateway 没有满足 R4.7 完整性、最小披露和数据面隔离要求的只读接口，因此必须先冻结可验收契约和实施边界。

## What Changes

- 新增 `GET /internal/v1/api-account-directory`，向专用 `relay_control_reader` service identity 返回完整、非分页、按 `accounts.id ASC` 排序的 API Account snapshot。
- 第一版成员严格限定为未软删除且 `type IN ('apikey', 'upstream')` 的 Sub2API Account；其他已知类型和未知未来类型 fail-closed 排除，Account 状态及 scheduler/runtime state 不影响成员资格。
- 每个条目只返回 `id`、`name`、`platform`、`type`、nullable sanitized `url` 和 persistent `status`；envelope 只返回 JSON integer `schema_version=1`、Gateway source/database snapshot time `generated_at` 和 `accounts`。
- 新增独立 service token 鉴权：Ops 使用 CSPRNG 生成至少 32 random bytes 并采用 unpadded Base64URL wire format；同时增加精确 route/method 授权、TLS 与 restricted management network 部署约束，以及带 4096-byte `credentials.base_url` ceiling 的最小数据库投影和 Secret/log/error 脱敏要求。
- 新增完整性、相互独立的响应大小与 Account 数、数据库查询时间、HTTP 总时间、并发和单 identity token-bucket hard limits；任何失败或超限都返回仅含 `code`、`message` 的明确 non-2xx，禁止部分成功、截断或 first-N。
- 所有 Directory 成功和失败响应设置 `Cache-Control: no-store`。
- 保证 Directory side-effect-free，并在资源竞争时优先失败或限流，不能改变或拖累 Sub2API 原生 AI 请求路由与调度。
- 第一版不引入 pagination、cache、ETag、revision/hash、delta、CDC、WebSocket、SSE、mTLS 或任何 Relay scheduler/topology 配置。

## Capabilities

### New Capabilities

- `api-account-directory`: 定义 Phase 4 只读 API Account Directory 的成员、响应契约、一致性、认证授权、最小披露、资源 hard limits、失败语义与数据面隔离。

### Modified Capabilities

无。

## Impact

- **阶段与依赖**：属于 Relay Station Phase 4，依赖 System Design v1.8 / R4.7 的 Directory、binding 与 duplicate ownership 决策，以及 ADR-0002 §2.2–2.5；后续 Control Directory ingestion 依赖本契约，但不在本 change 实现。
- **真相边界**：只读取 Sub2API 原生 `accounts` 持久化真相，不创建 Account、Group、binding、Relay Node、Cluster 或 scheduler truth；`accounts.id` 是唯一 binding identity reference。
- **后端**：后续实现预计影响 Gateway internal route、专用鉴权、配置/Secret、数据库 allowlist projection、URL sanitization、限流/并发控制、handler/service/repository 接线和相应测试。
- **身份与安全**：HTTP identity 为 `relay_control_reader`；Gateway 专用数据库运行身份为 `relay_directory_db_reader`；SECURITY DEFINER owner 为 NOLOGIN `relay_directory_definer`。Control 只持有 HTTP token，不得获得 Gateway 数据库 credential。
- **职责边界**：Gateway 负责 route、authentication、config hooks 和 default-disabled；Ops 负责生产 TLS、management ACL、public-ingress deny 和 Secret deployment。部署前置条件未满足时不得启用 Directory。
- **兼容性**：新增 internal GET endpoint，不修改现有公开 AI、用户或管理员 API，不修改 Sub2API Account/Group schema 语义或原生 routing/scheduling 行为。
- **数据面隔离**：Directory 不进入 AI request critical path；成功、失败、超时、限流或 Control 停止 polling 均不得改变 Account 状态、scheduler/cache 或请求转发。
- **回滚**：可通过撤销 internal route 暴露、停用独立 reader token/网络 ACL 和回退专用投影来移除能力，不需要迁移或恢复 Account、Group 或 scheduler state。
- **上游同步**：变更只增加 Relay Station 独立 internal capability；后续同步 Sub2API 上游时必须保留显式接线与测试，不向上游原生 scheduler 注入 Relay Station 语义。
- **架构门禁**：任何 Relay Cluster、P2C、Relay retry/failover/breaker/affinity/drain 或第二套 scheduler state 都超出本 change，必须重新进行架构评审。
