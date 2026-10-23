# 实现阶段索引

本页保留早期 M1–M6 的模块划分，帮助理解实现位置。当前版本与验证范围以 [STATUS.md](STATUS.md) 为准，具体能力见 [FEATURE_MATRIX.md](FEATURE_MATRIX.md)。

| 阶段 | 当前能力 | 仍需验证或扩展的范围 | 操作入口 |
|---|---|---|---|
| M1 资料与文本/向量 | 来源、文件版本、专项构建、字段/模型配置、索引读取 | 真实模型/来源、业务语料、大规模执行 | [来源与构建](OPERATIONS.md) |
| M2 图与组合 | 支撑、类型化关系/路径、邻接下推、组合/重排 | 真实 Neo4j、业务消歧与关系质量 | [检索](RETRIEVAL.md) |
| M3 Client/MCP | 三种 Client、JSON/SSE/stdio、资源/提示、外部 OAuth 资源验证 | 第三方 Agent/IdP、未实现可选协议、目标平台实机 | [API 与 MCP](API_MCP.md) |
| M4 运行与维护 | Key/密码入口、配置、反馈到题集、预算、快照与记录轮转、容器/CI | 业务容量与生产运维验收 | [日常管理](PRODUCT_OPERATIONS.md) |
| M5 知识与生命周期 | 草稿/Wiki/写回、在线版本清理、离线集合回收、已存投影恢复 | 真实模型/S3/后端恢复、孤儿发现、供应商联合备份 | [知识生命周期](LIFECYCLE_AND_SOURCES.md) |
| M6 Research/Recipe | 有界任务、固定依据、预算、检查点、明确续跑与状态 SSE | 真实调查质量、未知收费调用处理边界 | [调查与配方](RESEARCH.md) |

`0.10.4` 加入图片描述，提供 HTTP、CLI、MCP 入口与预算控制，见 [IMAGE_DESCRIBE.md](IMAGE_DESCRIBE.md)。功能实现、自动化检查和实际环境验收分别记录，不给出整体完成百分比。
