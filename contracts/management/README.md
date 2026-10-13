# Demo.2 管理接口

`registry.json` 记录本轮新增/扩展的管理路由。这里的接口不自动加入MCP的只读查询工具集，也不改变既有 `contracts/registry.json` 中查询动作的名称。

JSON Schema提供请求结构与示例；路径规范、KB范围、已存在的来源、相同请求幂等、字段之间的约束由Go操作层执行。source-policy中 every为0或至少10秒、watch为0或至少2秒且不能用于git；不是所有“数字在总范围内”的组合都可用。

代码位置：
- `internal/server/diagnostics.go`：题集、评测任务、查询历史。
- `internal/server/hook.go`：自定义HMAC事件。
- `internal/server/server.go`：来源策略、构建细节。
- `internal/store/operations.go`、`evaluation/types.go`：执行约束和持久对象。

新增返回结构为普通HTTP JSON；评测通过已有Job返回。暂不宣称已生成覆盖全部管理路由的完整OpenAPI或SDK。


## Demo.3

product-routes.json记录实际12个管理/辅助路径、17项方法组合（含已存在路径的新增方法）。provider-settings、key-create、feedback-resolution、confirm定义请求结构；Go操作层继续处理修订、来源、实际KB和时间条件。不是把所有管理动作加入MCP。

## Demo.4

`lifecycle-routes.json`记录5个新增路径和2个既有路径的扩展行为，新增source-create、wiki-generate/regenerate、sync-target-create、knowledge-sync/archive请求Schema。`scripts/lifecycle_contracts.py`可重建这些文档和示例；Go仍负责实际状态判断，未引入运行时通用Schema解释器。写回不自动加入只读MCP工具组。离线prune只有服务器本机命令，无Web远程删除入口。
