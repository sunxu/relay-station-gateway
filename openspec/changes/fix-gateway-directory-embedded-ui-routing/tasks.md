## 1. 后端命名空间分发

- [x] 1.1 在 `shouldBypassEmbeddedFrontend` 中加入 `/internal/` 前缀，并用现有 predicate 风格保持其它后端命名空间行为；通过对应 predicate 单元测试验证路径边界
- [x] 1.2 检查 middleware 顺序与 `RegisterDirectoryRoutes` 注册关系，确保 `/internal/` 请求调用 `Next()`；通过完整 router 测试验证已注册 route 能被命中

## 2. Embedded frontend 集成回归

- [x] 2.1 分别构造挂载真实 `FrontendServer.Middleware()` 与 `ServeEmbeddedFrontend()` 的 router 测试夹具，复用真实 `RegisterDirectoryRoutes` 和 Directory handler/service；通过测试证明 exact Directory GET 在正常完整 snapshot 下返回 HTTP 200、JSON envelope 且 Content-Type 不是 `text/html`
- [x] 2.2 在两套入口分别增加未知 `/internal/` 路径测试；验证后端返回 HTTP 404 且响应不包含嵌入式 `index.html`，不引入新的未知路径 JSON 契约
- [x] 2.3 在两套入口分别增加普通 SPA 路径与既有 `/api/`、`/v1/`、`/v1beta/`、`/backend-api/`、`/antigravity/`、`/setup/`、health/model/response/media 路径回归测试；验证既有分发行为保持

## 3. Directory 契约与安全负向验证

- [x] 3.1 在两套入口各自的完整 router 下验证 exact route 的非 GET、缺失 token、无效 token、disabled 场景分别返回 405/401/404 及既有错误 code；验证 handler 错误均为固定 JSON envelope 且 `Cache-Control: no-store`
- [x] 3.2 验证 limits、source v1、完整性、排序、redaction、safe URL 与数据面隔离测试仍通过；任何失败均为整请求 non-2xx，不得截断或部分成功
- [ ] 3.3 验证 `/internal/` bypass 不扩大 TLS、restricted management network、public ingress ACL 或 neighboring internal route 授权；补充安全负向测试证据

## 4. 交付验证

- [x] 4.1 运行 Gateway 相关单元、route、embed、Directory 集成测试及既有回归测试，从 `backend/` 执行 `go test -tags=embed ./internal/web ./internal/server/routes -count=1 -v`（集成测试若落在其它包则纳入实际包），记录两套入口的实际测试名称与 PASS、无 SKIP；默认无 embed tag 的测试不得替代。记录其它命令和结果；未执行的测试必须明确标为待执行
- [ ] 4.2 使用包含当前 Gateway revision 的镜像执行联合 exact route smoke，记录 status、稳定错误分类、Content-Type 与 `no-store`；确认运行镜像 revision 不再落后于工作树
- [ ] 4.3 执行并记录并发/突发请求下的 Directory hard-limit、超时、速率与 max-concurrency 验证，确认失败为整请求失败且 AI data plane 不受影响
- [ ] 4.4 执行性能隔离与资源优先级验证，确认 Directory 压力不会改变 Sub2API routing、scheduler、retry、breaker、affinity 或 drain 行为
- [ ] 4.5 检查无 migration、volume、runtime secret、Control 或 Ops 文件变更；执行 `openspec validate fix-gateway-directory-embedded-ui-routing --type change --strict --no-interactive` 与 `openspec validate --all --strict`、`git diff --check`，并确认 clean worktree 证据
