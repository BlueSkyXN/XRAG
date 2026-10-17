# 工作台内嵌资源

前端源码在 [webapp/](../../../webapp/)，完整开发步骤见 [DEVELOPMENT.md](../../../docs/DEVELOPMENT.md)。

在仓库根目录先执行 `make web`，生成本目录的 `dist/`；再执行 `make build` 将资源编入 Server。`dist/` 不进入 Git，构建会替换旧产物。Server 使用 `go:embed web`，本 README 保证没有前端资源时目录仍非空、Go 编译仍可进行。

前端开发用 `make web-dev`，Vite 5173 的 `/v1` 代理到 Server 8765。缺失 `dist/` 的 Server 在 `/` 返回 503，API/MCP 继续可用。Go 测试使用注入的前端 fixture。
