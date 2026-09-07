# 规划验证矩阵

本文件只记录规划阶段已核实的事实和实施后必须执行的验收。下表中标为“待执行”的项目不是已通过的测试。

| 编号 | 验证内容 | 证据或命令 | 状态 |
| --- | --- | --- | --- |
| P1 | middleware 使用 `shouldBypassEmbeddedFrontend`，当前未识别 `/internal/` | `backend/internal/web/embed_on.go`；`backend/internal/server/router.go` | 已核实代码事实 |
| P2 | Directory 路径注册在 `/internal/v1` | `backend/internal/server/routes/directory.go` | 已核实代码事实 |
| P3 | 现场请求曾返回 HTTP 200 `text/html` SPA；运行镜像 revision 与当前工作树不同 | Control 联合验收记录（仓库相对位置见下）；运行镜像 `c5f20f0bd5`、检查时 Gateway HEAD `f4c2a5918` | 已观察；旧镜像是否含 Directory handler 未证实 |
| P4 | 生产 router 有正常和 legacy fallback 两套 embedded 入口，共享 bypass predicate；现有 embed 测试受 build tag 控制 | `backend/internal/server/router.go:74`、`backend/internal/web/embed_on.go:87`、`backend/internal/web/embed_on.go:301`、`backend/internal/web/embed_test.go:1` | 已核实代码事实 |
| V1 | exact Directory 成功路径 | 完整 embedded middleware/router 测试；正常完整 snapshot，HTTP 200、JSON、非 HTML | PASS：对应实施/本地验收证据见下 |
| V2 | 非 GET、缺失/错误 token、disabled | 完整 router 测试；分别断言 405/401/404、固定 code、`Cache-Control: no-store` | PASS：对应实施/本地验收证据见下 |
| V3 | 未知 `/internal/` | backend 404 测试；断言非 SPA HTML，不新增未知路径 JSON 契约 | PASS：对应实施/本地验收证据见下 |
| V4 | SPA 与既有 backend namespace 回归 | embed/router 测试覆盖 SPA、`/api/`、`/v1/`、`/v1beta/`、`/backend-api/`、`/antigravity/`、`/setup/`、health/model/response/media | PASS：对应实施/本地验收证据见下 |
| V5 | source v1、limits、完整性、排序、redaction、safe URL、数据面隔离 | 既有 Directory 测试与本 change 回归命令 | PASS：对应实施/本地验收证据见下 |
| V6 | TLS、管理网、public ingress 与 neighboring internal route 安全负向 | 受限网络/认证集成验证 | PASS：对应实施/本地验收证据见下 |
| V7 | hard limits、超时、速率、并发和性能隔离 | Gateway 专项与联合压测/资源隔离验证 | 部分完成：limits/timeout/admission 专项及基本数据面可用性已验证；完整性能隔离仍待补证，见 Final Review |
| V8 | 镜像与回滚 | 对应 revision 镜像已构建、部署并 smoke；回滚准备为受保护 DB/config 备份与旧镜像 | 部署 PASS；未执行旧镜像回滚，不声称回滚演练通过 |
| V9 | 规划工件严格校验和范围 | `openspec validate fix-gateway-directory-embedded-ui-routing --type change --strict --no-interactive`、`git diff --check`、工作树检查 | PASS：change strict；all strict 3/3；diff check；仅新增本 change |

跨仓证据从 Gateway 仓库根目录读取：`../control/openspec/changes/add-gateway-directory-runtime-integration/planning-validation.md`，对应 Control 文档提交 `a11d415`。Control 当前仍为 22/27；本规划及后续路由修复不会自动关闭其联合验收任务。

## Embedded entrypoint acceptance

以下矩阵必须对两个真实入口分别执行，不以 predicate 单测或裸 Gin route 测试替代。使用真实 Directory route、handler/service；仅数据源按既有测试惯例提供 fixture。

| 场景 | `FrontendServer.Middleware()` | `ServeEmbeddedFrontend()` |
| --- | --- | --- |
| enabled、合法 token、完整 snapshot：200、既有 JSON envelope、非 HTML、no-store | PASS：settings | PASS：legacy |
| 非 GET：405 `directory_method_not_allowed`、固定 JSON、no-store | PASS：settings | PASS：legacy |
| enabled GET、缺失 token：401 `directory_unauthorized`、固定 JSON、no-store | PASS：settings | PASS：legacy |
| enabled GET、错误 token：401 `directory_unauthorized`、固定 JSON、no-store | PASS：settings | PASS：legacy |
| disabled GET：404 `directory_disabled`、固定 JSON、no-store | PASS：settings | PASS：legacy |
| 未注册 `/internal/` 路径：后端 404、非 SPA | PASS：settings | PASS：legacy |
| 普通 SPA 路径：既有 HTML 页面 | PASS：settings | PASS：legacy |
| 既有 backend namespace：分发保持 | PASS：settings | PASS：legacy |

专项计划命令（从 `backend/`）：`go test -tags=embed ./internal/web ./internal/server/routes -count=1 -v`。若集成夹具位于其它包，实施时一并记录实际包与命令。验收证据必须含两个入口对应测试名称、PASS 且无 SKIP；无 embed tag、无测试匹配或缺少嵌入资源不算通过。规划阶段未执行；实施阶段已执行，结果见下。

2026-09-07 设计评审 P2 修订：补齐两入口矩阵、真实 route/handler 要求及 embed tag 执行约束。仅修规划文档，实施任务保持未完成。

## Implementation evidence

2026-09-07：生产改动仅为 `backend/internal/web/embed_on.go` 的共享 predicate 增加 `/internal/`。`backend/internal/server/routes/directory_embed_test.go` 使用真实 route、service 与两个真实 frontend middleware；数据源使用固定只读 fixture。

- Red：临时移除该行，执行 `go test -tags=embed ./internal/server/routes -run '^TestEmbeddedDirectoryRouteMatrixBothFrontendEntrypoints$' -count=1`，settings/legacy 均因 `text/html` 而失败。恢复修复后执行下列完整专项。
- Green：从 `backend/` 执行 `go test -tags=embed ./internal/web ./internal/server/routes -count=1 -v`，PASS（web 0.788s，routes 2.606s），无 SKIP。`TestEmbeddedDirectoryRouteMatrixBothFrontendEntrypoints/settings` 与 `/legacy` 均 PASS；覆盖上述两入口矩阵。错误响应断言固定 JSON 与 no-store；四个边界 ID 以 `json.RawMessage` 逐字断言 source v1 numeric JSON，未经过 float64。
- `TestEmbeddedFrontendInternalNamespaceBoundary`：`/internal/` 及其子路径 bypass；`/internal`、`/internal-ui/`、`/internalized/` 不扩大匹配，PASS。
- 静态文件测试修正为当前真实 `dist/logo.svg`，保留 SVG MIME 与 cache header 断言；原 `/logo.png` 已不存在，未新增或修改前端资源。
- `go test ./internal/directory ./internal/config -count=1`：PASS（3.779s / 3.244s）。复用既有完整性、safe URL、redaction、timeout、rate、bulkhead、unrelated-route isolation 测试；这不是实际部署性能证明。
- 随后完成的实际镜像、入口 ACL 与共享数据面联合验收见下；未改变 TLS、认证、API、migration、Control 或 Ops 实现。

## Local deployment evidence

2026-09-07，本地 `relay-station-dev`：

- 代码 revision：`dfe4419233ef62bf7f9a4c9a4b5a7bff9802165d`，构建前工作树干净。标准命令：`docker build --pull=false --build-arg COMMIT=dfe4419233ef62bf7f9a4c9a4b5a7bff9802165d --build-arg VERSION=dfe441923 --label org.opencontainers.image.revision=dfe4419233ef62bf7f9a4c9a4b5a7bff9802165d -t relay-station/gateway:dfe441923 .`，PASS；前端使用相同输入的 Docker cache，后端重新构建。
- 镜像：`relay-station/gateway:dfe441923`，ID `sha256:57b03961e744fe2b4c7d39dd9e9fe4c639eaf910cdfde52ba1bd972da20d4198`。运行容器 image/revision label 完全匹配且 healthy。后续仅验收文档提交不会改变此源码对应关系。
- 发布前完成受保护 Gateway `pg_dump -Fc` 与配置备份；Compose 仅重建 Gateway，保留 volumes、reader token、证书及 ingress ACL，无 migration。回滚可恢复备份中的 image/config；本轮未执行旧镜像回滚，未声称回滚演练通过。
- 临时本地验收脚本：`/Volumes/DevRAM/tmp/relay-gateway-routing-release.py`，SHA256 `7f8f5f031f707339855a44eb89203cb1cb7f6e4982941c738dd716cc0a59509d`。依次执行 `backup`、`deploy relay-station/gateway:dfe441923`、`check false`、`source true`、`check true`、`burst`、`source false`、`check false`，均 exit 0。该脚本是本地编排辅助，不是新的生产入口；Secret 从受保护 runtime 读取，输出仅状态和聚合，不保存 source 或推理原始响应。
- 管理请求使用 `https://localhost:18444`，加载现有 CA，证书及 hostname 验证开启；公开请求走 `http://127.0.0.1:18082`。没有使用跳过证书校验或改变 ingress 策略来绕过本修复。

| 实际请求/状态 | 结果 |
| --- | --- |
| disabled GET exact Directory（合法、缺失、错误 token） | 404 `directory_disabled`，JSON，no-store |
| enabled GET + 合法 token | 200，source v1 精确 envelope/Account 字段集，1 个 Gateway Account，Python int 解码 numeric ID，无浮点转换 |
| enabled GET + 缺失/错误 token | 401 `directory_unauthorized`，JSON，no-store |
| POST exact Directory（两种开关状态） | 405 `directory_method_not_allowed`，JSON，no-store |
| 管理入口未知 `/internal/v1/unregistered-routing-check` | 404，非 SPA |
| public exact Directory | 403，公开入口隔离保持 |
| reader token 请求 `/api/v1/admin/accounts` | 401，不获得管理员权限 |
| public UI `/` 与 `/health` | 200 |

### Resource isolation evidence

`go test ./internal/directory ./internal/config -count=1` 的全包通过覆盖账户数/响应字节上限、查询/总预算、token bucket、max-concurrency 和 whole-request error；其中 repository 使用 sqlmock，阻塞 service fixture 证明两个被占用的 Directory 槽会导致新 Directory 请求 429，而独立路由仍可用。它们不等同于真实 PostgreSQL 压测或真实 AI 延迟测量。

另执行实际共享栈 burst：10 worker、40 个合法 Directory 请求，与一个真实 Gateway → Node chat completion 并行；结果 2×200、38×429，最长 0.094s。每个响应均验证 JSON/no-store，成功必须完整 envelope，429 必须是既有 rate/busy 分类，不接受 HTML 或 partial success。另有一次无 burst 的真实推理作观测基线：HTTP 200，3.018s；burst 同期推理 HTTP 200，7.041s。未保存推理内容。

这组证据证明本次有界突发场景下 Directory admission 有效、AI 调用仍可用；不证明推理延迟完全不变，也不构成容量或 P95/P99 SLO 基准。原生 routing/scheduler/retry/breaker/affinity/drain 行为依据生产 diff 仅一行 frontend namespace dispatch、既有独立路由隔离测试及真实成功调用核对，没有修改对应实现或配置。

收尾：Gateway 保持修复镜像 healthy；Gateway Directory 与 Control Directory 均恢复/保持 false。Node 账号文件仍 6 个。Control/ops 工作树干净且未修改；Control change 仍为 22/27，后续联合 ingestion/Binding 验收须在其自身 change 继续，不自动关闭。

最终验证：change strict PASS、all strict 3/3 PASS、`git diff --check` PASS。最终提交只含 tasks 与本验收文档；提交后复核工作树。当时标记 13 项完成；后续 Final Review 重新打开 4.4，当前 12/13。未 archive 或 push。

## Final Review

REQUEST CHANGES，P1=0、P2=1。双入口真实 handler/router、HTTP 契约、命名空间边界与部署 smoke 充分；未发现生产路由修复缺陷。

P2：此前 V7 整体 PASS、4.4 完成状态超出了实际证据。一次 baseline/一次 burst 下 AI 成功、限流有界，以及生产 diff 未改变调度实现，不能单独证明压力下所有原生 scheduler/retry/breaker/affinity/drain 行为与性能隔离要求。现有延迟样本仅为观测，不是统计对照实验。

4.4 重新打开，当前 12/13；V7 改为部分完成。已有测试及部署结果保留，不放宽测试断言、不修改生产实现、不宣称新增实测。补齐 4.4 的明确压力条件、可复用原生行为断言与对照结果后才能关闭；若要收窄原验收范围，须显式评审该调整，不能在 evidence 中静默豁免。Control 联合验收暂未继续，仍 22/27。
