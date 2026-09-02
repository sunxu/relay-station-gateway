## 1. 冻结实现基线与契约测试

- [ ] 1.1 核对 pinned Sub2API 版本中的 Account schema、SoftDeleteMixin、type constants 和 `credentials.base_url` 来源，把任何偏差先回写本 change，并以 `openspec validate add-api-account-directory --type change --strict --no-interactive` 验证
- [ ] 1.2 为成功 envelope、精确六字段、空目录、成员 allowlist 和 ID 升序建立失败优先的 contract tests，并验证测试在实现前按预期失败
- [ ] 1.3 为 non-2xx 固定 classification、无 partial body 和第一版排除字段建立 contract tests，并验证所有 specs 场景都有测试映射

## 2. 建立最小数据库投影

- [ ] 2.1 新增不可变 SQL migration，创建固定 owner/search_path 的 SECURITY DEFINER Directory function，并用 migration test 验证只投影六字段所需 scalar
- [ ] 2.2 在 migration 中创建 `relay_control_reader` database role，撤销 PUBLIC 权限并只授予精确 function execute，使用 privilege test 验证直接读取 `accounts`、`credentials` 和其他业务表失败
- [ ] 2.3 实现单 statement 的 source time、空集合 sentinel、成员过滤和 `id ASC` 查询，并用 Repository test 验证 `generated_at` 与 rows 来自同一 snapshot
- [ ] 2.4 给专用 projection 增加 `deleted_at`、已知排除类型和未知类型测试，并验证 persistent status 与 scheduler/runtime 字段变化不影响成员资格

## 3. 接入独立配置、Secret 与数据库资源

- [ ] 3.1 新增默认关闭的 Directory config，覆盖 enabled、current/previous token Secret 和专用数据库连接参数，并用 config test 验证环境变量解析和 Secret 不被序列化
- [ ] 3.2 创建最多 2 个连接、0 idle connection 的专用只读 `sql.DB` provider，并用构造测试验证不会回退主业务 pool
- [ ] 3.3 增加启动配置校验：enabled 时要求至少 256-bit token、专用数据库配置和部署 prerequisite 标记，并用 table test 验证缺失项 fail-closed
- [ ] 3.4 实现 current/previous token rotation window 配置校验，并验证窗口外 previous token 被拒绝且 token 内容不进入错误或日志

## 4. 实现 Directory 垂直模块

- [ ] 4.1 创建 `backend/internal/directory/` 的最小 config、auth、repository、sanitizer、service 和 handler 边界，并用依赖检查验证不引用 Account service、scheduler cache、Redis、outbox、Group repository 或 upstream client
- [ ] 4.2 实现 Bearer header 大小/格式校验和 constant-time token comparison，并用 auth tests 覆盖有效、缺失、错误、过期和 application credential 复用
- [ ] 4.3 实现 `credentials.base_url` scalar 的 origin-only sanitizer，并用 table tests 覆盖 HTTP(S)、大小写、显式 port、IPv6、userinfo、path、query、fragment、非字符串、非法 scheme 和 malformed URL
- [ ] 4.4 实现 projection row 校验，拒绝非正数/重复/非递增 ID 和非法 required scalar，并验证 malformed optional URL 只变为 null
- [ ] 4.5 实现 `schema_version="1"`、UTC RFC 3339 `generated_at` 和精确 item DTO，并用 JSON key-set test 验证成功 body 无 wrapper 或额外字段

## 5. 实现准入、完整响应与错误语义

- [ ] 5.1 实现每分钟 10 请求、burst 2 的单 identity non-blocking token bucket，并用确定性 clock test 验证正常 polling、burst、恢复和 fail-closed
- [ ] 5.2 实现容量 2 的 non-blocking semaphore，并用并发 test 验证第三个请求在取得数据库连接前返回 `directory_busy`
- [ ] 5.3 实现 3 秒总 timeout 和 2 秒 query timeout，并用可控阻塞 Repository test 验证取消传播与两个稳定 failure classification
- [ ] 5.4 实现 10,000 Account ceiling，并用 10,000/10,001 边界测试验证超限整体 413、无 first-N body
- [ ] 5.5 实现 4 MiB bounded encode-before-write，并用边界测试验证超限整体 413、序列化失败不写 200 或 partial JSON
- [ ] 5.6 实现稳定脱敏 error mapping，并用底层错误 canary test 验证 body/log 不包含 SQL、relation、DB host、stack、raw URL 或 Secret

## 6. 注册 route 与部署边界

- [ ] 6.1 只注册 `GET /internal/v1/api-account-directory` 并接入 Directory 专用 middleware，使用 route tests 验证其他 method、neighboring internal route 和 admin/user credential 均无授权
- [ ] 6.2 将 Directory provider 接入现有 composition 和 lifecycle，并用启动/关闭 test 验证 disabled 模式不创建专用 pool、enabled 模式正确关闭资源
- [ ] 6.3 更新示例部署配置和 Secret 文档，明确 TLS、management network ACL、public ingress deny 与 token rotation，并通过配置示例检查验证不含真实 Secret
- [ ] 6.4 增加 ingress smoke test，验证 public AI ingress 不路由 `/internal/v1/*`，management ingress 必须同时满足 TLS、source ACL 和 service auth

## 7. 完成验收与证据

- [ ] 7.1 运行完整 Directory contract suite，验证 positive、empty、membership、exact fields、ordering、complete-vs-partial 和 first-version exclusion 场景
- [ ] 7.2 运行 security-negative suite，使用 credentials/extra/proxy/error/URL/header canary 验证数据库权限、HTTP body、日志和指标均无 Secret leakage
- [ ] 7.3 运行 concurrent-mutation integration test，验证 create/update/soft-delete/restore 并发时每个成功响应只反映单一 MVCC snapshot
- [ ] 7.4 运行 hard-limit suite，验证 Account count、response bytes、query timeout、HTTP timeout、max concurrency 和 identity rate 的精确边界与 whole-request failure
- [ ] 7.5 运行 performance-isolation test，在 AI 数据面负载和 Directory retry storm 并发时验证专用 pool/bulkhead 生效且数据面不等待 Directory
- [ ] 7.6 运行 `make test-backend` 和 `make build`，确认现有 Account/Group、admin API、Gateway routing/scheduling 和 upstream synchronization regression 全部通过
- [ ] 7.7 更新 change verification evidence，逐条关联 Requirement、测试、SQL privilege probe、ingress smoke result 和脱敏日志样例，并再次运行 OpenSpec strict validation
- [ ] 7.8 对照 System Design v1.8 / R4.7 与 ADR-0002 reconciliation，确认未引入 Cluster/P2C/Relay scheduler/retry/breaker/affinity/drain、Control 实现或计费功能
- [ ] 7.9 运行 `git diff --check`、检查 change scope 和 `git status --short`，确认实现提交不包含临时 Secret、生成物、无关改动或未解释的 dirty worktree
