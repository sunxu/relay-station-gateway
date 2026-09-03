## 1. 冻结实现基线与契约测试

- [x] 1.1 核对 pinned Sub2API 版本中的 Account schema、SoftDeleteMixin、type constants、现有 `*sql.DB` provider 和 `credentials.base_url` 来源，并以 OpenSpec strict validation 验证无契约漂移
- [x] 1.2 为 JSON integer `schema_version=1`、精确六字段、空目录、成员 allowlist、未知 platform/status string 和 ID 升序建立失败优先的 contract tests
- [x] 1.3 为精确 `{"code","message"}` non-2xx envelope、所有响应 `Cache-Control: no-store`、无 partial body 和第一版排除字段建立 contract tests

## 2. 实现单条最小 raw SQL projection

- [x] 2.1 创建只接收现有 `*sql.DB` 的 Directory repository，并用构造/依赖测试验证数据库 schema、访问身份、credential 和连接配置均未改变
- [x] 2.2 实现单 statement 的 database source time、空集合 sentinel、`deleted_at IS NULL`、type allowlist、`id ASC` 和 `LIMIT 10001`，并用 sqlmock 验证完整 SQL shape 与参数
- [x] 2.3 在 SQL projection 内实现 `base_url` JSON string type 与 4096-byte ceiling，并用 query-shape tests 验证缺失、非 string、4096/4097-byte 边界只返回有界 scalar 或 NULL
- [x] 2.4 验证 projection result 只包含 `generated_at/id/name/platform/type/url_source/status` scalar，不 materialize 完整 credentials、extra、Account relation、Ent Account entity 或完整 Account DTO
- [x] 2.5 增加 SQL safety test，拒绝 INSERT、UPDATE、DELETE、MERGE、DDL、`FOR UPDATE`、advisory lock 和多 statement，并验证查询不触发 scheduler/account mutation
- [x] 2.6 增加 membership、空目录、10,000/10,001 overflow、正 ID、唯一和严格升序测试

## 3. 接入独立 HTTP 配置与认证

- [x] 3.1 新增默认关闭的 Directory config，只覆盖 enabled、unpadded Base64URL current/previous token 和 rotation window，并验证未新增数据库连接配置
- [x] 3.2 使用 unpadded Base64URL 解码 current/previous token并校验 decoded length 至少 32 bytes；测试 malformed encoding、padding、31/32-byte 边界和 rotation window
- [x] 3.3 实现 `Authorization: Bearer <base64url-token>` 校验和 decoded-byte constant-time comparison，覆盖有效、malformed、short、缺失、错误、过期和 application credential 复用
- [x] 3.4 验证 encoded/decoded token 不进入配置序列化、错误、日志或指标

## 4. 实现 Directory 垂直模块

- [x] 4.1 创建最小 auth、repository、sanitizer、service、handler 边界，并用 dependency test 验证不引用 Account service/repository、Ent Account、scheduler cache、Redis、outbox、Group repository 或 upstream client
- [x] 4.2 实现有界 URL scalar 的 origin-only sanitizer，覆盖 HTTP(S)、显式 port、IPv6、userinfo、path、query、fragment、非法 scheme 和 malformed URL
- [x] 4.3 实现 projection row/whole-snapshot 校验，拒绝非法 required scalar、非正数、重复、非递增 ID，同时让 malformed URL 降级 null、未知 platform/status string 原样保留
- [x] 4.4 实现 JSON integer `schema_version=1`、UTC RFC 3339 `generated_at` 和精确 item DTO，并验证 string `"1"`、wrapper 和额外字段被拒绝
- [x] 4.5 使用 credentials/raw URL/SQL/DB error canary 验证 result、成功/失败 body、日志无泄漏；Directory 不产出独立指标时记为 N/A

## 5. 实现准入、完整响应与有界资源占用

- [x] 5.1 实现 refill 10 tokens/minute、capacity 2、cost 1/request 的单 identity non-blocking token bucket，并用确定性 clock test 验证精确语义和 fail-closed
- [x] 5.2 实现容量 2 的 non-blocking semaphore，并验证第三个请求在调用共享数据库前立即返回 `directory_busy`
- [x] 5.3 在 authentication 成功后立即启动 3 秒 total deadline，将 DB deadline 设为 `min(queryStart+2s,totalDeadline)`，并验证连接等待、查询和编码均受预算约束
- [x] 5.4 实现独立 10,000 Account ceiling 和 4 MiB bounded encode-before-write，并用独立/交叉边界测试验证 whole-request failure
- [x] 5.5 实现仅含 `code`、`message` 的稳定 error mapping 和所有路径 `Cache-Control: no-store`
- [x] 5.6 在共享 DB pool 压力与 retry storm 下验证 Directory 受 admission/timeout 限制、额外资源占用有界，AI request 不等待 Directory-specific state/lock，且没有 scheduler/account mutation

## 6. 注册 route 与部署边界

- [x] 6.1 让 `GET /internal/v1/api-account-directory` 走 Directory handler，并让其他 method 在同一路径返回固定错误 envelope
- [x] 6.2 将 Directory 接入现有 composition/lifecycle，验证 disabled 模式不执行查询且没有独立数据库资源需要创建或关闭
- [x] 6.3 更新 Gateway config hooks 与 Ops 部署文档，明确 Gateway 负责 route/auth/default-disabled，Ops 负责 CSPRNG token、TLS、management ACL、public-ingress deny 和 Secret deployment
- [x] 6.4 执行 ingress smoke test，验证 public AI ingress 拒绝 `/internal/v1/*`，management ingress 同时要求 TLS、ACL 和 service auth，Control 配置不含 Gateway DB credential

## 7. 完成验收与证据

- [x] 7.1 运行 Directory contract、SQL projection、Secret canary 与 hard-limit tests，并记录 evidence
- [x] 7.2 运行 `openspec validate add-api-account-directory --type change --strict --no-interactive`
- [x] 7.3 运行 `make test-backend` 和 `make build`，确认现有 Account/Group routing/scheduling 与 Gateway 数据面回归通过
- [x] 7.4 对照 System Design v1.8 / R4.7 与 ADR-0002，确认未引入 Control、binding、duplicate ownership、Cluster/P2C/Relay scheduler/retry/breaker/affinity/drain 或计费功能
- [x] 7.5 更新 verification evidence，逐条关联 Requirement、测试、query-shape 和 canary 结果，不保存 raw Secret或数据库错误
- [x] 7.6 运行 `git diff --check` 并检查 `git status --short`，确认只包含已解释的 Gateway Directory 实现与 OpenSpec evidence
