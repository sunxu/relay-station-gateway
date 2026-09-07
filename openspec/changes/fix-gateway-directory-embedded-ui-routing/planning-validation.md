# 规划验证矩阵

本文件只记录规划阶段已核实的事实和实施后必须执行的验收。下表中标为“待执行”的项目不是已通过的测试。

| 编号 | 验证内容 | 证据或命令 | 状态 |
| --- | --- | --- | --- |
| P1 | middleware 使用 `shouldBypassEmbeddedFrontend`，当前未识别 `/internal/` | `backend/internal/web/embed_on.go`；`backend/internal/server/router.go` | 已核实代码事实 |
| P2 | Directory 路径注册在 `/internal/v1` | `backend/internal/server/routes/directory.go` | 已核实代码事实 |
| P3 | 现场请求曾返回 HTTP 200 `text/html` SPA；运行镜像 revision 与当前工作树不同 | Control 联合验收记录（仓库相对位置见下）；运行镜像 `c5f20f0bd5`、检查时 Gateway HEAD `f4c2a5918` | 已观察；旧镜像是否含 Directory handler 未证实 |
| P4 | 生产 router 有正常和 legacy fallback 两套 embedded 入口，共享 bypass predicate；现有 embed 测试受 build tag 控制 | `backend/internal/server/router.go:74`、`backend/internal/web/embed_on.go:87`、`backend/internal/web/embed_on.go:301`、`backend/internal/web/embed_test.go:1` | 已核实代码事实 |
| V1 | exact Directory 成功路径 | 完整 embedded middleware/router 测试；正常完整 snapshot，HTTP 200、JSON、非 HTML | 待执行 |
| V2 | 非 GET、缺失/错误 token、disabled | 完整 router 测试；分别断言 405/401/404、固定 code、`Cache-Control: no-store` | 待执行 |
| V3 | 未知 `/internal/` | backend 404 测试；断言非 SPA HTML，不新增未知路径 JSON 契约 | 待执行 |
| V4 | SPA 与既有 backend namespace 回归 | embed/router 测试覆盖 SPA、`/api/`、`/v1/`、`/v1beta/`、`/backend-api/`、`/antigravity/`、`/setup/`、health/model/response/media | 待执行 |
| V5 | source v1、limits、完整性、排序、redaction、safe URL、数据面隔离 | 既有 Directory 测试与本 change 回归命令 | 待执行 |
| V6 | TLS、管理网、public ingress 与 neighboring internal route 安全负向 | 受限网络/认证集成验证 | 待执行 |
| V7 | hard limits、超时、速率、并发和性能隔离 | Gateway 专项与联合压测/资源隔离验证 | 待执行 |
| V8 | 镜像与回滚 | 构建当前 revision 镜像，执行 exact route smoke；记录回滚后无持久数据变化 | 待执行 |
| V9 | 规划工件严格校验和范围 | `openspec validate fix-gateway-directory-embedded-ui-routing --type change --strict --no-interactive`、`git diff --check`、工作树检查 | PASS：change strict；all strict 3/3；diff check；仅新增本 change |

跨仓证据从 Gateway 仓库根目录读取：`../control/openspec/changes/add-gateway-directory-runtime-integration/planning-validation.md`，对应 Control 文档提交 `a11d415`。Control 当前仍为 22/27；本规划及后续路由修复不会自动关闭其联合验收任务。

## Embedded entrypoint acceptance

以下矩阵必须对两个真实入口分别执行，不以 predicate 单测或裸 Gin route 测试替代。使用真实 Directory route、handler/service；仅数据源按既有测试惯例提供 fixture。

| 场景 | `FrontendServer.Middleware()` | `ServeEmbeddedFrontend()` |
| --- | --- | --- |
| enabled、合法 token、完整 snapshot：200、既有 JSON envelope、非 HTML、no-store | 待执行 | 待执行 |
| 非 GET：405 `directory_method_not_allowed`、固定 JSON、no-store | 待执行 | 待执行 |
| enabled GET、缺失 token：401 `directory_unauthorized`、固定 JSON、no-store | 待执行 | 待执行 |
| enabled GET、错误 token：401 `directory_unauthorized`、固定 JSON、no-store | 待执行 | 待执行 |
| disabled GET：404 `directory_disabled`、固定 JSON、no-store | 待执行 | 待执行 |
| 未注册 `/internal/` 路径：后端 404、非 SPA | 待执行 | 待执行 |
| 普通 SPA 路径：既有 HTML 页面 | 待执行 | 待执行 |
| 既有 backend namespace：分发保持 | 待执行 | 待执行 |

专项计划命令（从 `backend/`）：`go test -tags=embed ./internal/web ./internal/server/routes -count=1 -v`。若集成夹具位于其它包，实施时一并记录实际包与命令。验收证据必须含两个入口对应测试名称、PASS 且无 SKIP；无 embed tag、无测试匹配或缺少嵌入资源不算通过。此命令本轮未执行。

2026-09-07 设计评审 P2 修订：补齐两入口矩阵、真实 route/handler 要求及 embed tag 执行约束。仅修规划文档，实施任务保持未完成。

## Implementation evidence

2026-09-07：生产改动仅为 `backend/internal/web/embed_on.go` 的共享 predicate 增加 `/internal/`。`backend/internal/server/routes/directory_embed_test.go` 使用真实 route、service 与两个真实 frontend middleware；数据源使用固定只读 fixture。

- Red：临时移除该行，执行 `go test -tags=embed ./internal/server/routes -run '^TestEmbeddedDirectoryRouteMatrixBothFrontendEntrypoints$' -count=1`，settings/legacy 均因 `text/html` 而失败。恢复修复后执行下列完整专项。
- Green：从 `backend/` 执行 `go test -tags=embed ./internal/web ./internal/server/routes -count=1 -v`，PASS（web 0.788s，routes 2.606s），无 SKIP。`TestEmbeddedDirectoryRouteMatrixBothFrontendEntrypoints/settings` 与 `/legacy` 均 PASS；覆盖上述两入口矩阵。错误响应断言固定 JSON 与 no-store；四个边界 ID 以 `json.RawMessage` 逐字断言 source v1 numeric JSON，未经过 float64。
- `TestEmbeddedFrontendInternalNamespaceBoundary`：`/internal/` 及其子路径 bypass；`/internal`、`/internal-ui/`、`/internalized/` 不扩大匹配，PASS。
- 静态文件测试修正为当前真实 `dist/logo.svg`，保留 SVG MIME 与 cache header 断言；原 `/logo.png` 已不存在，未新增或修改前端资源。
- `go test ./internal/directory ./internal/config -count=1`：PASS（3.779s / 3.244s）。复用既有完整性、safe URL、redaction、timeout、rate、bulkhead、unrelated-route isolation 测试；这不是实际部署性能证明。
- 尚待实际镜像、入口 ACL 与共享数据面联合验收；未改变 TLS、认证、API、migration、Control 或 Ops 实现。
