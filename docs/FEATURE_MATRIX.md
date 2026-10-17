# 当前功能索引

保留原 F01–F66 编号，补充主线未发布的 F67。表格描述当前实现和操作边界；版本、测试环境与已知问题统一见 [STATUS.md](STATUS.md)。

## 资料与构建

| ID | 功能 | 当前行为 |
|---|---|---|
| F01 | Server/Client 分离 | 两个 Go 程序；Client 可纯 Go 构建 |
| F02 | Worker | 内置或独立同机 Worker；SQLite 持久任务队列 |
| F03 | 无模型启动 | 关键词、原文、显式关系与基础 Recipe 可用 |
| F04 | 组件开关 | Web、MCP、内置 Worker 可分别关闭 |
| F05 | 创建/列举 KB | ID 与展示名分开，按身份列可见库 |
| F06 | KB 管理 | 改名、逻辑删除、保留 ID、独立处理配置 |
| F07 | 上传 | MD/Markdown/TXT 内容上传 |
| F08 | 来源登记 | path、builtin、git、s3、weknora |
| F09 | 来源编辑 | include/exclude、停用和移除；kind/root/mount 固定绑定 |
| F10 | 本机目录 | 完整枚举与读取，失败保留旧发布内容 |
| F11 | 本机 Git | 固定 commit 读取树/blob，不读取未提交改动 |
| F12 | 公共远程 Git | HTTPS mirror；无私有 SSH/凭据助手或 LFS 展开 |
| F13 | S3 来源 | path-style、SigV4、完整分页、条件读取与前后比对 |
| F14 | WeKnora 来源 | 分页导入完成文档的已解析 text chunks |
| F15 | 过滤和字段 | 来源 glob；string/number/boolean/date 类型 |
| F16 | 解析与中文输入 | 轻量解析、NFC、分词/词典；可选 CommonMark/YAML 插件 |
| F17 | incremental/full | 增量差异或全部重新物化；有效模型缓存可复用 |
| F18 | 专项构建 | vectors_only 基于发布内容；graph_only 复用已有向量 |
| F19 | 构建触发 | 手动/API、秒数/cron、watch、custom/GitHub/GitLab/Gitea Hook |
| F20 | 文件版发布 | active edition 与固定 ref；普通更新、撤回和清理各有语义 |
| F21 | 缓存 | 增量识别与模型输入缓存 |
| F22 | optional 投影 | 配置允许时发布正文并记录向量/图缺失状态 |
| F23 | 任务系统 | KB 互斥、排队、取消、幂等；Research/Recipe 可明确续跑 |
| F24 | 模型预算 | 预留/结算、调用/Token/金额上限与 UTC 汇总；含主线图片描述 |

操作见 [来源与构建](OPERATIONS.md)、[连接配置](CONNECTIONS.md)。

## 检索与接入

| ID | 功能 | 当前行为 |
|---|---|---|
| F25 | 关键词/编号 | Server FTS5 严格匹配与精确块计数；正文按页取 |
| F26 | 文件树与原文 | 元数据树、按 path 当前文件、按 ref 固定版本和片段 |
| F27 | grep | 完整原文 RE2，可跨块；total 单位 matches |
| F28 | within/字段 | 固定引用范围与请求过滤，不能隐式扩域 |
| F29 | scope.expand | 显式扩大实体提及范围，继续应用 filters |
| F30 | handle/result/cursor | 缓存有序身份并按原版本翻页；窗口/期限/撤回检查 |
| F31 | +ask | 问句词处理、宽松词法、可选语义/图/重排；RRF 排名融合 |
| F32 | +context | 实体、关系、出处与相关已发布 Wiki |
| F33 | 外部搜索 | 具名 XRAG peer 的实时检索和引用阅读 |
| F34 | Discover | 当前配置与资料能力说明；无模型健康探测调用 |
| F35 | Trace/资源 | 实际路线与候选摘要；索引/compact/原生 direct 读取路径 |
| F36 | Embedding | OpenAI 兼容 embeddings；SQLite 精确余弦或 Qdrant |
| F37 | Rerank | 显式 rerank 接口和已配置模型 |
| F38 | Qdrant | 向量/词法稀疏索引、过滤、物理集合及 descriptor |
| F39 | Neo4j | Query API v2 写入和按图前沿读取关系支撑 |
| F40 | 图查询 | 实体、邻居、路径、提及块；保留方向与来源 |
| F41 | 图计数 | relations 与 support_count 分别记录 |
| F42 | LLM 抽图 | 按库模型、类型/路径过滤、原文 evidence 校验 |
| F43 | 插件 | parser/extractor 命名注册与 JSONL；按调用进程，无市场/沙箱/进程池 |
| F44 | 按库模型配置 | 每库完整 providers、字段、词典、图与解析配置 |
| F45 | 向量代际与 GC | 登记模型身份/物理集合，保留期与持有者检查后离线回收空集合 |
| F46 | server Client | XRAG Key、发布视图、读取与持久任务 |
| F47 | local Client | MD/TXT 只读扫描；ask OR/BM25；最多 20,000 文件/50 MiB |
| F48 | direct Client | 后端 binding、稀疏词法/语义候选、按点取正文；旧集合可回退并提示重建 |
| F49 | Profile 与输出 | JSON/table、dry-run、配置隔离、输出文件拒绝覆盖 |
| F50 | HTTP/MCP | JSON/SSE/stdio、取消、资源/提示、可选外部 OAuth 资源验证 |
| F51 | MCP 写工具 | 显式 write 组的草稿/反馈提交，受 Contributor 权限限制 |

操作见 [检索指南](RETRIEVAL.md)、[API 与 MCP](API_MCP.md)。

## 知识、评测与维护

| ID | 功能 | 当前行为 |
|---|---|---|
| F52 | 知识生命周期 | 草稿、编辑、修订、明确发布与归档 |
| F53 | 依据与适用范围 | 文档/图/路径/聚合引用归档，applies_to/not_applies_to |
| F54 | Wiki | 生成/再生成，默认大纲后正文，范围内多路取材，最多 40 块 |
| F55 | Wiki 链接 | 唯一已发布主题链接与 context 聚合，改名后需重建引用页 |
| F56 | path/Git 写回 | 专用目标、摘要/SHA 条件检查，保留冲突信息 |
| F57 | S3/push/auto_sync | 条件 PUT、显式 Git push、发布事务 outbox 与 Worker 同步 |
| F58 | 反馈处理 | 分页、修订匹配、状态处理、转测试问题草稿 |
| F59 | 题集与策略 | 批准后发布不可变题集、family/purpose、导出和同题比较 |
| F60 | 诊断与留出 | 读取保存 trace/当前文件；字面题去重，无完整历史重放或语义盲评 |
| F61 | 角色/Key | Reader/Contributor/Maintainer/Admin，范围与期限，永久 Admin 保护 |
| F62 | 工作台/设置 | 按角色导航、概览、接入、配置、Key/密码登录入口 |
| F63 | 备份/恢复 | 导出、SQLite 快照、新目录隔离恢复、已存外部投影恢复 |
| F64 | 维护 | 备份计划、记录轮转、在线内容批次、离线 prune/集合 GC |
| F65 | 构建交付 | Linux amd64 完整包、4 个纯 Client 包、源码、校验和、GHCR/HF workflow |
| F66 | Research/Recipe | 单 KB 有界任务、依据/数字绑定、检查点、明确续跑与任务 SSE |
| F67 | 图片描述（未发布） | 专用 HTTP、CLI 与 MCP；单 KB 权限、大小校验与预算用量见图片指南 |

操作见 [知识生命周期](LIFECYCLE_AND_SOURCES.md)、[日常管理](PRODUCT_OPERATIONS.md)、[数据恢复](DATA_RECOVERY.md)、[历史维护](HISTORY_MAINTENANCE.md)、[调查](RESEARCH.md) 和 [图片描述](IMAGE_DESCRIBE.md)。
