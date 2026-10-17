# 当前版本、验证范围与已知问题

文档核对日期：2026-10-18。本轮从 `79e74c2` 核对近期改动，补齐图片描述的 CLI、MCP 与预算接入，并修复 Server 读取旧 Qdrant 集合的兼容问题。发行版本、存储格式和主线未发布内容分别列出，供升级和接入时判断。

## 版本与兼容范围

| 项目 | 当前值 / 含义 |
|---|---|
| 已发布版本 | [v0.10.3](https://github.com/BlueSkyXN/XRAG/releases/tag/v0.10.3)，2026-10-16 发布，7 个附件 |
| 源码版本常量 | `core.Version=0.10.3`；主线可含版本常量尚未增加的变更 |
| 主线未发布功能 | 图片描述 HTTP/CLI/MCP 路径与预算用量，见 [IMAGE_DESCRIBE.md](IMAGE_DESCRIBE.md) |
| SQLite 存储标记 | `store_schema=demo9`；更早 Demo 存储无自动迁移 |
| 常规检索响应 | `schema_version=0.7-demo`；不是发行号 |
| MCP 协议 | 2025-11-25 / 2025-06-18 / 2025-03-26 的已实现能力 |
| 工作台 | React 工作台；前端资源需在 Go 编译前构建 |

## 主要能力

- MD/TXT 上传、目录/Git/S3/WeKnora 来源，来源策略、原生 Git Hook 与四种构建模式。
- 文件级发布、固定引用、关键词/语义/关系检索、范围、分页、组合与外部 XRAG peer。
- server/local/direct 三种 Client，HTTP 与 MCP JSON/SSE/stdio、resources/prompts、可选外部 OAuth 资源验证。
- 知识草稿、修订、Wiki、发布、归档、目录/Git/S3 写回与显式自动同步。
- 反馈、题集、策略对比、诊断、模型预算，单 KB 有界 Research/Recipe 与明确检查点续跑。
- SQLite 快照、自动备份/记录轮转、在线内容批次清理、离线退休集合回收与已存投影恢复。

详细映射见 [FEATURE_MATRIX.md](FEATURE_MATRIX.md)，操作入口见 [文档总览](README.md)。

## 验证依据

以下为按日期可追溯的记录，不把历史执行结果当成每个未来部署的状态。

| 对象 | 可核验依据 | 覆盖边界 |
|---|---|---|
| 图片功能首次合入的 CI 基线 | [ci，79e74c2](https://github.com/BlueSkyXN/XRAG/actions/runs/38907271155)，2026-10-18 核查为成功 | Go 测试、前端构建、Python 检查、工作台 HTTP 契约、打包/解包等 workflow 内检查 |
| 图片功能首次合入的容器基线 | [image，79e74c2](https://github.com/BlueSkyXN/XRAG/actions/runs/38907270878)，核查为成功 | 容器启动、登录模式、示例/Git 检索、HF 包装入口与镜像提交检查 |
| 发布附件 | v0.10.3 的源码、Linux amd64、4 个 Client 包、SHA256SUMS | Client 交叉构建与附件存在；其他平台实机仍需分别验收 |
| 最近可见 HF 部署记录 | [publish-space，b42e6f3](https://github.com/BlueSkyXN/XRAG/actions/runs/38881263085)，核查为成功 | 该次部署的镜像提交核对；新主线图片功能未由这条记录覆盖 |
| 文档示例（2026-10-17） | 临时本机实例记录覆盖关键词、ask、文件树、原文、Recipe、local 与图片专用 HTTP/CLI | 合成语料；图片模型为受控端点，真实模型效果未测 |
| 大语料流程 | [corpus-eval，b42e6f3](https://github.com/BlueSkyXN/XRAG/actions/runs/38880277810)，核查为成功 | 可复现合成语料基线；不能推算业务召回率或容量 SLA |
| 专业后端/商业模型 | 适配代码、协议测试、[联调脚本](LIVE_INTEGRATION.md) | 仍需记录目标版本、真实数据、凭据环境和联合运行结果 |

本机原始检查记录保存在忽略的 `local/`；公开源码附带可重跑脚本，历史 `checks/REPORT.md` 等路径不作为当前交付必需文件。

## 图片描述验证

[接入回归测试](../tests/image_describe_integration_test.go) 覆盖 CLI 显式 `--kb` 与 JSON 输入、HTTP/stdio MCP、响应 Schema、大小预检查、权限拒绝、预算发送前拦截及用量记录。使用受控 Chat 端点验证协议与控制行为；真实模型识别效果和供应商兼容性仍需验证。工作台暂无专用图片上传入口，调用方式与限制见 [图片描述](IMAGE_DESCRIBE.md)。

[旧集合回归测试](../tests/qdrant_collection_compat_test.go) 覆盖旧物理集合保留，以及同库新旧集合并存时的 Server 语义查询。测试使用受控 Qdrant 协议端点。

## 实际使用边界

| 领域 | 当前边界 |
|---|---|
| 格式 | 直接来源为 MD/Markdown/TXT；PDF/Office/OCR/任意业务数据库导入未提供 |
| 规模 | Server 每来源 5,000 文件/64 MiB；local 20,000 文件/50 MiB；单文件均 5 MiB；其余查询窗口见检索指南 |
| direct | 已知 XRAG 布局、相同资料域、指定物理集合；不继承 Server 发布、撤回和权限视图 |
| 语义与图 | SQLite 向量为精确扫描；图深度/前沿/支撑有界；模型语义、消歧与业务因果需核对 |
| 生命周期 | 本机锁/租约；无多机 direct 读租约、孤儿集合自动发现或跨后端原子快照 |
| 调查 | 单 KB、固定范围/预算/期限；未知收费调用拒绝重放；无通用 DAG/业务写工具 |
| MCP / OAuth | 无 sampling/Tasks/subscriptions/完整会话重放；OAuth 只验证外部 token，第三方 Agent/IdP 需实际联调 |
| 部署 | Linux 完整包与容器有 CI 路线；macOS/Windows Server、其他架构和生产并发需各自验证 |

## 后续验证入口

真实后端用 [LIVE_INTEGRATION.md](LIVE_INTEGRATION.md)，业务检索效果用自己的 development/holdout 题集，部署用 [staging](../deploy/hf/STAGING.md) 与固定 digest。验收分别记录版本、环境、数据、操作结果和未覆盖项。
