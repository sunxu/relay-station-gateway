## Why

这是 Phase 4 联合验收前的 Gateway 路由可达性修复。当前 Gateway 的嵌入式前端 middleware 在已注册 Directory route 之前运行，`shouldBypassEmbeddedFrontend` 没有识别 `/internal/` namespace；工作树代码因此会把该路径当作 SPA 路由返回 `index.html`（HTTP 200、`text/html`），Control 将其判定为 `contract_invalid`。现场运行镜像与当前工作树 revision 不同，旧镜像是否包含 Directory handler 尚未证实，需在部署验证中区分。

## What Changes

- 让 `shouldBypassEmbeddedFrontend` 将 `/internal/` 视为后端 API 路径并交给已注册 route。
- 增加带 embedded frontend middleware 的完整 router 回归验证，确认 Directory 返回自身 JSON 契约。
- 验证未知 `/internal/...` 路径由后端返回 404，且不是 SPA HTML；不新增未知路径的错误 JSON 契约。
- 验证正常 SPA 路由仍返回前端页面，既有 API namespace 行为不变。
- 记录当前部署镜像与工作树 revision 不同；实现和部署必须使用包含本修复的 Gateway 镜像。

## Capabilities

### New Capabilities

### Modified Capabilities

- `api-account-directory`: 修改 exact Directory route 在 embedded frontend middleware 下的可达性和错误响应行为；沿用既有 source v1、认证、limits、数据面隔离和安全契约。

## Impact

- 操作结果：Control 调用 exact Directory endpoint 时到达 Gateway Directory handler，并获得既有 JSON success/error contract，而不是 SPA HTML。
- 依据：Relay Station System Design v1.8 / R4.7 的 Gateway Directory 与部署边界，以及 ADR-0002 关于 Gateway 保持 Sub2API 原生数据面职责的约束。
- 代码：`backend/internal/web/embed_on.go` 及其 frontend/router 集成测试。
- HTTP：只影响 `/internal/` 后端 namespace 的 dispatch；不改变 Directory handler、SQL projection、token、TLS 或 public-ingress ACL。
- 依赖：需要重新构建并部署 Gateway 镜像；Control、Gateway 数据库和 migration 不变。
- 兼容性与回滚：增加后端路径 bypass，不改变 SPA、静态资源和既有 `/api/` 等 bypass；可通过回滚 Gateway 镜像恢复旧行为，不涉及持久数据回滚。
- 边界：不引入 Relay topology、scheduler、P2C、retry/breaker/affinity/drain 或新的 Gateway 配置。
