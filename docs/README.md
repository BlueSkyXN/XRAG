# XRAG 文档总览

本目录面向当前主线：发布版 `0.10.3`，以及 `main` 中标注为未发布的功能。版本、存储标记、现场验证和已知问题集中在 [STATUS.md](STATUS.md)。首次运行从 [项目 README](../README.md) 开始。

## 按任务找文档

| 你要做什么 | 阅读入口 | 主要内容 |
|---|---|---|
| 理解系统怎么组成 | [架构图解](ARCHITECTURE.md) | 组件、构建发布、存储、固定引用 |
| 导入并持续更新资料 | [来源与构建](OPERATIONS.md) | 目录/Git、触发策略、构建模式、任务与评测 |
| 找资料并核对出处 | [检索指南](RETRIEVAL.md) | keyword、ask、semantic、图、分页、统计 |
| 连接模型或数据库 | [连接与配置](CONNECTIONS.md) | 配置优先级、Embedding/Chat/Rerank、direct |
| 给脚本或 Agent 接入 | [API 与 MCP](API_MCP.md) | 动作映射、身份、工具组、OAuth、协议边界 |
| 执行多步调查 | [Research 与 Recipe](RESEARCH.md) | 任务、预算、固定范围、检查点与续跑 |
| 描述图片内容 | [图片描述](IMAGE_DESCRIBE.md) | 未发布 HTTP/CLI/MCP 功能、大小限制与用量 |
| 整理与发布知识 | [知识生命周期与外部来源](LIFECYCLE_AND_SOURCES.md) | 草稿、Wiki、依据、写回、S3/WeKnora |
| 管理服务 | [日常管理](PRODUCT_OPERATIONS.md) | 工作台、登录、角色、Key、配置、容器 |
| 备份和恢复 | [数据恢复](DATA_RECOVERY.md) | 导出、SQLite 快照、新目录恢复、外部投影 |
| 控制历史占用 | [历史维护](HISTORY_MAINTENANCE.md) | 缓存、记录、在线内容批次、离线集合回收 |
| 编译、检查与发版 | [开发指南](DEVELOPMENT.md) | 目录、构建、测试、语料评测、Release/镜像 |
| 部署到 Hugging Face | [部署说明](../deploy/hf/README.md) · [staging](../deploy/hf/STAGING.md) | 固定 digest、持久卷、平台入口、验收与回退 |
| 联调专业后端 | [真实联调](LIVE_INTEGRATION.md) | 专用测试实例、脚本、副作用、结果记录 |
| 查能力与版本差异 | [功能表](FEATURE_MATRIX.md) · [状态](STATUS.md) · [更新记录](CHANGELOG.md) | 功能索引、验证依据、版本变化 |

## 示例和契约

- [examples/](../examples/) 提供配置和请求模板。`examples/docs/` 是合成检索语料；其中的编号、路径、分页样本用于测试。
- [查询动作注册表](../contracts/registry.json)、[动作 Schema](../contracts/actions/)、[管理契约说明](../contracts/management/README.md) 给出机器可读结构。
- [Agent 检索技能](../skills/xrag-retrieval/SKILL.md) 说明如何选择和组合工具。
- [实现阶段索引](COMPLETION.md) 保留 M1–M6 规划与当前模块的对应关系。

JSON 模板中的 revision、ID、路径和服务地址必须替换成实际值。密钥使用执行端可解析的 `env:` / `file:` 引用。请求 Schema 和 Go 处理器共同决定实际约束。

## 图解阅读

图以 `mermaid` 代码块保存在 Markdown 中，不依赖单独图片。架构、检索、知识发布、调查检查点、备份恢复、维护和发布流程各有对应图解；每幅图附近均有操作或边界说明。

## 历史材料

以下文件保留对应批次的设计背景、增量和当时结果；执行当前操作时使用上面的专题指南。历史文档中的版本号、路径和未实现项按当时语境阅读。

| 材料 | 历史主题 |
|---|---|
| [Demo.5](DEMO5_OPERATIONS.md) | 按库配置、字段、图、知识依据与写回 |
| [Demo.6](DEMO6_OPERATIONS.md) | 索引读取、维护、反馈形成题集 |
| [Demo.7](DEMO7_OPERATIONS.md) | 元数据查询、退休集合、原生 Git Hook |
| [Demo.8](DEMO8_OPERATIONS.md) | direct、在线内容清理、投影恢复、调查 |
| [Demo.9](DEMO9_OPERATIONS.md) | MCP 传输、OAuth、检查点与任务状态 |
| [Demo.6 资源实验](RESOURCE_PROBE.md) · [Demo.7 资源实验](RESOURCE_PROBE7.md) | 特定合成样本的资源观测 |
| [早期历史目录](history/README.md) | Demo.1–Demo.3 的说明和报告 |

`checks/` 和 `local/` 为忽略目录，历史文字中的这些路径指原执行环境产物，下载源码后通常不可用。可复查的当前验证入口与公开 CI 链接见 [STATUS.md](STATUS.md)。
