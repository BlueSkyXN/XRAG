# internal/server/web

`dist/` 里是工作台前端（`webapp/`）的构建产物，由 `make web` 生成，不进 Git。
服务端用 `go:embed web` 打包 `dist/`；本 README 保证目录非空，没有产物时也能编译。

- 构建产物：`make web`（写入 `dist/`，整体替换）
- 前端源码与开发模式：见仓库根 README 和 `webapp/`

`dist` 缺失时启动的服务没有界面（`/` 返回 503 提示），API 和 MCP 不受影响。
