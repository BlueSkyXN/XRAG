# 管理契约索引

本目录包含管理路由清单和 JSON Schema。清单按功能加入批次拆分，文件中的 Demo 版本表示契约形成时点；当前产品版本见 [STATUS.md](../../docs/STATUS.md)。这些文件不是一份完整自动生成的 OpenAPI/SDK。

| 清单 | 主要内容 |
|---|---|
| [registry.json](registry.json) | 来源策略、构建详情、评测和查询历史 |
| [product-routes.json](product-routes.json) | 概览、Key、服务配置、反馈、快照和资料导出 |
| [lifecycle-routes.json](lifecycle-routes.json) | Wiki 再生成、修订、写回、归档和远程来源 |
| [completion-routes.json](completion-routes.json) | KB 设置/用量、来源编辑、peer、草稿/反馈提交 |
| [iteration6-registry.json](iteration6-registry.json) | 自动维护、反馈转题集、批准与诊断 |

Schema 描述结构、基本类型与范围；Go 处理器另检查角色、KB、实际引用、revision、幂等键、状态和字段组合。例如来源 watch 仅用于 path/builtin，部分时间字段只允许 0 或指定正值区间。

## 当前实现入口

- [server.go](../../internal/server/server.go)：基本 KB/来源/任务与读取入口。
- [product.go](../../internal/server/product.go)：Key、服务设置、反馈、导出与备份。
- [lifecycle.go](../../internal/server/lifecycle.go)、[complete.go](../../internal/server/complete.go)：知识生命周期、KB 配置与外部来源。
- [iteration6.go](../../internal/server/iteration6.go)：维护、题集草稿与诊断。
- [research.go](../../internal/server/research.go)、[task_events.go](../../internal/server/task_events.go)：Research/Recipe、状态、取消、续跑与 SSE。
- [hook.go](../../internal/server/hook.go)、[native_hook.go](../../internal/server/native_hook.go)：自定义与原生 Git Hook。

常规查询动作在 [../registry.json](../registry.json) 和 [../actions/](../actions/)，显式写工具与调查动作分别由 [write.go](../write.go)、[tasks.go](../tasks.go) 登记。图片描述使用专用请求/响应，使用方式见 [图片说明](../../docs/IMAGE_DESCRIBE.md)。

管理动作不会因存在 HTTP 路由自动成为 MCP 工具。MCP 暴露范围与角色另见 [API_MCP.md](../../docs/API_MCP.md)。离线 prune、vector-gc、recover-projections 为 Server 本机命令。

## 检查与维护

运行 `python3 scripts/check_management.py` 校验已有 Schema/示例，需要 Python jsonschema。修改契约时同时检查处理器、CLI、工作台、示例与测试。

`scripts/management_contracts.py`、`scripts/lifecycle_contracts.py` 等为特定批次的生成脚本；运行前核对它们输出的文件与版本，避免用历史生成器覆盖后续契约。当前使用说明从 [文档总览](../../docs/README.md) 进入。
