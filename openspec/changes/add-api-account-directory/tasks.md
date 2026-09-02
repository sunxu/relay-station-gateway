## 1. 冻结实现基线与契约测试

- [ ] 1.1 核对 pinned Sub2API 版本中的 Account schema、SoftDeleteMixin、type constants 和 `credentials.base_url` 来源，把任何偏差先回写本 change，并以 `openspec validate add-api-account-directory --type change --strict --no-interactive` 验证
- [ ] 1.2 为 JSON integer `schema_version=1` 成功 envelope、精确六字段、空目录、成员 allowlist、未知 platform/status string 和 ID 升序建立失败优先的 contract tests，并验证 string `"1"` 被拒绝
- [ ] 1.3 为精确 `{"code","message"}` non-2xx envelope、所有响应 `Cache-Control: no-store`、无 partial body 和第一版排除字段建立 contract tests，并验证所有 specs 场景都有测试映射

## 2. 建立最小数据库投影

- [ ] 2.1 验证现有 migration executor 是否具备创建/管理 Directory roles、function ownership 和 grant/revoke 所需权限并记录前置结果；若不足，定义 Ops 受控 role bootstrap，验证方案不扩大 `relay_directory_db_reader` 权限
- [ ] 2.2 新增不可变 SQL migration，创建 NOLOGIN `relay_directory_definer` owner、LOGIN `relay_directory_db_reader` runtime role 和固定 search_path 的 SECURITY DEFINER function，并用 role/migration tests 验证身份属性
- [ ] 2.3 撤销 PUBLIC 权限并只向 `relay_directory_db_reader` 授予精确 function execute，使用 privilege test 验证直接读取 `accounts`、`credentials` 和其他业务表失败
- [ ] 2.4 在数据库 projection 中实现 `base_url` JSON string type 与 4096-byte ceiling，并用 SQL test 验证缺失、非 string、4096/4097-byte 值只按契约返回有界 scalar 或 NULL
- [ ] 2.5 实现单 statement 的 source time、空集合 sentinel、成员过滤、`id ASC` 和 `LIMIT 10001` bounded overflow sentinel，并用 Repository test 验证同一 snapshot 与第 10,001 行整体失败
- [ ] 2.6 给专用 projection 增加 `deleted_at`、已知排除类型和未知类型测试，并验证 persistent status 与 scheduler/runtime 字段变化不影响成员资格

## 3. 接入独立配置、Secret 与数据库资源

- [ ] 3.1 新增默认关闭的 Directory config，覆盖 unpadded Base64URL current/previous token Secret 和专用数据库连接参数，并用 config test 验证环境变量解析、相同 rotation wire format 和 Secret 不被序列化
- [ ] 3.2 创建使用 `relay_directory_db_reader`、最多 2 个连接、0 idle connection 的专用只读 `sql.DB` provider，并用构造测试验证不会回退主业务 pool 或向 Control 暴露 credential
- [ ] 3.3 使用 unpadded Base64URL 解码 current/previous token 并校验 decoded length 至少 32 bytes；用 table test 验证 malformed encoding、padding、31/32-byte 边界和无运行时熵估计
- [ ] 3.4 实现 current/previous token rotation window 配置校验，并验证两者使用相同 wire format、窗口外 previous token 被拒绝且 encoded/decoded token 不进入错误或日志

## 4. 实现 Directory 垂直模块

- [ ] 4.1 创建 `backend/internal/directory/` 的最小 config、auth、repository、sanitizer、service 和 handler 边界，并用依赖检查验证不引用 Account service、scheduler cache、Redis、outbox、Group repository 或 upstream client
- [ ] 4.2 实现 `Authorization: Bearer <base64url-token>` 大小/scheme、unpadded Base64URL decode、至少 32 decoded bytes 和 decoded-byte constant-time comparison，并用 auth tests 覆盖有效、malformed Base64URL、padding、short token、缺失、错误、过期和 application credential 复用
- [ ] 4.3 实现有界 `credentials.base_url` scalar 的 origin-only sanitizer，并用 table tests 覆盖 HTTP(S)、大小写、显式 port、IPv6、userinfo、path、query、fragment、缺失、非字符串、4096/4097 bytes、非法 scheme 和 malformed URL
- [ ] 4.4 实现 projection row 校验，拒绝非正数/重复/非递增 ID 和非法 required scalar，并验证 malformed optional URL 只变为 null、未知 platform/status string 原样保留
- [ ] 4.5 实现 JSON integer `schema_version=1`、UTC RFC 3339 `generated_at` 和精确 item DTO，并用 JSON type/key-set test 验证 string `"1"` 被拒绝且成功 body 无 wrapper 或额外字段

## 5. 实现准入、完整响应与错误语义

- [ ] 5.1 实现 refill 10 tokens/minute、capacity 2、cost 1/request 的单 HTTP identity non-blocking token bucket，并用确定性 clock test 验证连续 refill、容量上限、cost、立即拒绝和 fail-closed
- [ ] 5.2 实现容量 2 的 non-blocking semaphore，并用并发 test 验证第三个请求在取得数据库连接前返回 `directory_busy`
- [ ] 5.3 在 authentication 成功后立即启动 3 秒 total deadline，并将 DB deadline 设为 `min(queryStart+2s,totalDeadline)`；用可控 clock/Repository test 验证 admission 时间计入总预算和取消传播
- [ ] 5.4 实现独立 10,000 Account ceiling，并用 10,000/10,001 边界测试验证 SQL sentinel 超限整体 413、无 first-N body
- [ ] 5.5 实现独立 4 MiB bounded encode-before-write，并用 byte 边界及 count/byte 交叉测试验证任一 limit 可单独拒绝、两者均未触发才继续
- [ ] 5.6 实现仅含 `code`、`message` 的稳定脱敏 error mapping，并用 exact key-set 与底层错误 canary test 验证无 metadata/details/reason、SQL、relation、DB host、stack、raw URL 或 Secret
- [ ] 5.7 在 Directory 最外层 middleware 设置 `Cache-Control: no-store`，并用 success、method、auth、rate、timeout 和 internal-error tests 验证所有响应路径

## 6. 注册 route 与部署边界

- [ ] 6.1 只注册 `GET /internal/v1/api-account-directory` 并接入 Directory 专用 middleware，使用 route tests 验证其他 method、neighboring internal route 和 admin/user credential 均无授权
- [ ] 6.2 将 Directory provider 接入现有 composition 和 lifecycle，并用启动/关闭 test 验证 disabled 模式不创建专用 pool、enabled 模式正确关闭资源
- [ ] 6.3 更新 Gateway config hooks 与 Ops 部署文档，明确 Gateway 负责 route/auth/default-disabled，Ops 负责 CSPRNG 至少 32 random bytes、unpadded Base64URL 编码、TLS、management ACL、public-ingress deny 和 Secret deployment，并检查未满足前置条件时保持 disabled
- [ ] 6.4 增加 ingress smoke test，验证 public AI ingress 不路由 `/internal/v1/*`，management ingress 必须同时满足 TLS、source ACL 和 service auth，且 Control 配置不含 Gateway DB credential

## 7. 完成验收与证据

- [ ] 7.1 运行完整 Directory contract suite，验证 positive、empty、membership、exact fields、ordering、complete-vs-partial 和 first-version exclusion 场景
- [ ] 7.2 运行 security-negative suite，使用 credentials/extra/proxy/error/URL/header canary 验证数据库权限、HTTP body、日志和指标均无 Secret leakage
- [ ] 7.3 运行 concurrent-mutation integration test，验证 create/update/soft-delete/restore 并发时每个成功响应只反映单一 MVCC snapshot
- [ ] 7.4 运行 hard-limit suite，验证 4096-byte URL projection、Account count/response bytes 独立及交叉边界、剩余 total deadline、max concurrency 和 token-bucket 精确语义与 whole-request failure
- [ ] 7.5 运行 performance-isolation test，在 AI 数据面负载和 Directory retry storm 并发时验证专用 pool/bulkhead 生效且数据面不等待 Directory
- [ ] 7.6 运行 `make test-backend` 和 `make build`，确认现有 Account/Group、admin API、Gateway routing/scheduling 和 upstream synchronization regression 全部通过
- [ ] 7.7 更新 change verification evidence，逐条关联 Requirement、测试、SQL privilege probe、ingress smoke result 和脱敏日志样例，并再次运行 OpenSpec strict validation
- [ ] 7.8 对照 System Design v1.8 / R4.7 与 ADR-0002 reconciliation，确认未引入 Cluster/P2C/Relay scheduler/retry/breaker/affinity/drain、Control 实现或计费功能
- [ ] 7.9 运行 `git diff --check`、检查 change scope 和 `git status --short`，确认实现提交不包含临时 Secret、生成物、无关改动或未解释的 dirty worktree
