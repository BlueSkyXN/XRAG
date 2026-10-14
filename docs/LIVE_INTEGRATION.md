> 当前为Demo.8。脚本使用全新测试库；模型/后端由运行者明确提供。报告必须区分真实数据库与模拟模型。

# 专业后端真实联调：脚本已交付，本轮未现场通过

本环境不能解析外部下载地址，也没有Docker及运行中的Qdrant/Neo4j。已尝试外部二进制下载但失败。没有用模拟服务冒充实际数据库，也没有调用真实商业模型。本轮live报告为 `checks/demo8/live-backends.json: not_run`。

## 1. 准备环境

在你自己的开发环境部署专用测试Qdrant及支持Query API v2的Neo4j，准备真实Embedding服务。不要直接拿重要现有库进行首次实验。

`deploy/compose.backends.yaml` 只包含外部数据库；XRAG 镜像用 CI 发布到 GHCR（`ghcr.io/blueskyxn/xrag`），联调时按 digest 或版本标签固定，不使用 floating latest：

```bash
# 由使用者填已选定、支持相应API的版本或digest
export QDRANT_IMAGE='qdrant/qdrant:已选定版本'
export NEO4J_IMAGE='neo4j:已选定社区版版本'
export QDRANT_API_KEY='你自己的测试Key'
export NEO4J_PASSWORD='你自己的测试口令'
docker compose -f deploy/compose.backends.yaml up -d
```

只绑定localhost端口。此Compose在本轮只解析过YAML，未进行镜像拉取或Docker运行；镜像及端点API需要实际选择验证。

## 2. 配置与执行

复制 `examples/providers.live.json` 到自己的配置，修改Embedding地址、模型、维度、修订。Qdrant使用HTTP6333，Neo4j本适配使用HTTP7474 Query API，不是Bolt URL。实际secret通过env/file解析。

```bash
make build
python3 scripts/live_backends.py \
  --config /绝对路径/real.providers.json \
  --report /绝对路径/live-report.json \
  --confirm-services
```

无confirm时不启动实例、不发任何外部请求，产生not_run报告并退出2。有confirm会调用Embedding并写入外部独立测试命名空间，可能产生费用。

脚本不读取你既有XRAG数据目录，自己创建临时目录、新origin和随机KB。它将执行：

1. 构建两份合成资料，SQLite + Qdrant + Neo4j真实组合。
2. 关键词、语义、组合检索、路径、原文引用。
3. 保存使用当前实际物理集合的direct profile。
4. 停止XRAG Server。
5. 用独立Client direct继续语义查询、读取及关系查询。

只生成连接、范围和流程检查，不把两份样本当成真实相关性评测。脚本未通过实际服务运行时，不可从“文件存在”推断以上步骤已经通过。

## 3. 外部数据清理

脚本为方便排查保留外部测试产物，报告含 `origin`、`kb`、`physical_collection`。不要删除共享的`xrag_demo_meta`，它可能保存其他KB描述。只在确认是本次临时origin后清理匹配记录/集合；脚本不自动跨库删除。

Neo4j图仍只保存实体、支撑和块标识；正文在SQLite/Qdrant。使用者取得直连数据库账号后，其访问由后端决定，不再继承XRAG Key的KB范围。

## 4. 依据与限制

本文只是联调步骤。实际库版本、模型响应、Schema、网络行为由报告决定：

- Qdrant Collections / Points：https://qdrant.tech/documentation/manage-data/collections/ 、https://qdrant.tech/documentation/concepts/points/
- Neo4j Query API：https://neo4j.com/docs/query-api/current/query/
- Qdrant原子alias不能与SQLite描述更新混称跨系统事务；当前使用具体物理集合、专项准备与发布；离线退休集合清理与同版投影恢复已有代码，真实联合运行仍需执行。

S3/WeKnora分别按LIFECYCLE_AND_SOURCES登记自己的测试实例。协议测试不替代实际实例版本和数据测试。
