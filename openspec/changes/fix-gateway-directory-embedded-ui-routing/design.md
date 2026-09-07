## Context

现有 Directory handler 已由 `RegisterDirectoryRoutes` 注册到 `/internal/v1`，但 embedded frontend middleware 在 route 注册前安装。`shouldBypassEmbeddedFrontend` 当前按 namespace 判断后端路径，缺少 `/internal/`，导致未知内部路径和已知 Directory path 都可能进入 SPA fallback。既有 `openspec/specs/api-account-directory/spec.md` 是 source v1 的权威契约，本 change 只修复 dispatch。

## Goals / Non-Goals

**Goals:**

- 让 `/internal/` 请求在 embedded frontend middleware 中调用 `Next()`，由 Gin backend routing 决定结果。
- 让 exact Directory path 继续使用既有 handler 的 GET、认证、disabled 和 `no-store` 契约。
- 用完整 router 验证 authorized success 为 JSON、method/token/disabled negative 为既有固定状态码和 code。
- 验证未知 `/internal/` path 为 404 且不是 SPA HTML，正常 SPA path 仍由 frontend fallback 服务。

**Non-Goals:**

- 不修改 Directory SQL、projection、source schema、token、limits、rate/concurrency 或 data-plane isolation。
- 不放宽 TLS、restricted management network、public ingress ACL 或任何 neighboring internal route authorization。
- 不修改 Control、Ops、Gateway database、Sub2API routing/scheduling 或任何 Relay topology 行为。

## Decisions

1. **扩展现有 bypass predicate。** 在 `shouldBypassEmbeddedFrontend` 中加入 `/internal/` prefix。复用现有 namespace predicate，避免为单一路径增加特殊分支；比在 Directory handler 中补救更早失败，因为 frontend middleware 当前会直接结束请求。
2. **保留 route 与 handler 的职责。** `RegisterDirectoryRoutes` 继续注册 `Any`，由既有 Directory service 返回 405/401/404；本 change 不把 frontend middleware 变成第二套认证或错误处理器。
3. **增加完整 middleware 回归测试。** 现有 route tests 使用裸 Gin router，不能覆盖 embedded middleware。测试应构造带 frontend middleware 的 router，检查 Directory success/negative 的 content type、状态码、固定 error code，并检查未知 `/internal/` 不返回 `index.html`。同时保留 SPA path 和已有 backend namespace 的回归断言。必须分别挂载真实 `FrontendServer.Middleware()` 和 legacy fallback `ServeEmbeddedFrontend()`，让两套入口执行同一组成功、405/401/404、未知内部路径和普通 SPA 断言；不可只验证共享 predicate 或以一套入口替代另一套。夹具复用真实 `RegisterDirectoryRoutes` 与 Directory handler/service，不用返回固定 JSON 的替代 handler。
4. **部署按镜像 revision 验证。** 当前现场镜像 revision 与工作树 HEAD 不同，旧镜像是否包含 Directory handler 不作推断。实现后必须构建带当前 Gateway revision 的镜像再验证；不通过配置绕过，也不改变 Control 的 source/auth 契约。

## Risks / Trade-offs

- **[Risk] `/internal/` 下未知路径的响应从 SPA 变为 backend 404，可能暴露此前被错误隐藏的 route 缺失。** → 这是 backend namespace 的预期 dispatch；只验证 404 非 HTML，不新增未知内部路径 JSON 契约。
- **[Risk] bypass prefix 可能改变未来内部页面路径。** → `/internal/` 已是 backend-only namespace；新增页面必须显式使用 frontend path，不依赖该 namespace 的 SPA fallback。
- **[Risk] 运行旧镜像会继续返回 HTML。** → 部署前比较镜像 label 与 Gateway HEAD，使用包含修复的镜像；回滚只回滚 Gateway 镜像，不触碰数据。
- **[Risk] middleware/router 集成测试依赖 embedded frontend 构建。** → 从 `backend/` 执行 `go test -tags=embed ./internal/web ./internal/server/routes -count=1 -v`，若最终集成测试位于其它包则同时纳入实际包。确认输出包含两套入口的实际测试且未 SKIP。默认 `go test ./...` 不包含 embed 测试，不能替代本项；缺少嵌入资源时使用项目既有前端构建流程准备，未执行则验收保持未完成。

## Migration Plan

1. 实现 predicate 与完整 router regression tests。
2. 运行 Gateway 相关单元、route、embed 和集成测试，确认既有 Directory spec 的 auth/limits/data-plane 测试未回归。
3. 构建带当前 revision 的 Gateway 镜像；在受控联合验收中将 `DIRECTORY_ENABLED` 设为启用以验证成功路径，并单独验证 disabled 负向路径，reader token、TLS 和 ingress ACL 保持不变，验收后恢复约定的配置状态。
4. 回滚时停止使用修复镜像并恢复上一兼容镜像；无 migration、volume 或 runtime secret 变更。
