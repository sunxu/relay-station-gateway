## ADDED Requirements

### Requirement: Directory 后端命名空间绕过嵌入式 UI 回退

Gateway SHALL 在任何嵌入式前端回退前，将每个 `/internal/` 请求交给已注册的后端路由。这样 exact Directory 路径 SHALL 暴露既有 API 契约，未知内部路径 SHALL 永远不作为 SPA HTML 提供。

#### Scenario: Exact Directory 路径到达 handler

- **WHEN** Directory 已启用且通过认证的 reader 发送 `GET /internal/v1/api-account-directory`，并存在正常完整 snapshot
- **THEN** Gateway 返回既有 Directory JSON envelope 和 HTTP 200，继续遵守既有 status、认证、source、limits、redaction 语义，响应 Content-Type 不是 `text/html`

#### Scenario: Directory 负向契约仍可见

- **WHEN** Directory 已启用且 caller 以 exact 路径发送非 GET、缺少 reader token 或无效 reader token，或 Directory 未启用时 caller 以 exact 路径发送 GET
- **THEN** 已注册 Directory handler 分别返回 HTTP 405 `directory_method_not_allowed`、HTTP 401 `directory_unauthorized`、HTTP 404 `directory_disabled`；这些 handler 错误继续使用既有固定 JSON error envelope 和 `Cache-Control: no-store`

#### Scenario: 未知内部路径不是 SPA 页面

- **WHEN** caller 请求未注册的 `/internal/` 路径
- **THEN** Gateway 从后端路由返回 HTTP 404，且不返回嵌入式前端 `index.html`；本场景不新增错误 JSON 契约

#### Scenario: 普通 SPA 路径仍是前端页面

- **WHEN** caller 请求后端命名空间之外的普通已注册 SPA 路径
- **THEN** Gateway 按既有行为继续提供嵌入式前端页面

#### Scenario: 既有后端命名空间保持不变

- **WHEN** caller 请求既有 `/api/`、`/v1/`、`/v1beta/`、`/backend-api/`、`/antigravity/`、`/setup/`、health、model、response、image 或 video 路径
- **THEN** Gateway 保持既有路由分发和响应行为
