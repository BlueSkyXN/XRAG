> 当前版本为 Demo.5。本文件保留对应模块的原操作说明；新增行为或冲突以 [DEMO5_OPERATIONS.md](DEMO5_OPERATIONS.md) 与 [STATUS.md](STATUS.md) 为准。不要求旧软件迁移。

# 可选连接与运行模式

**先使用无模型基础版，再配置实际服务。以下均为模板，不含可用凭据。**

## 0. Demo.4 的两种配置来源

默认不带 `--config` 时，Server/Worker读取“设置与帮助”保存的数据库配置。保存只写配置，不调用模型；需重启Server和全部Worker。可导出字段配置，但只含env/file引用。

显式 `--config` 或 `XRAG_DEMO_CONFIG` 优先，界面只展示不编辑；下文文件方式继续可用。JSONL抽取器仍只从这个文件配置。没有热切换、自动重建或保存时付费探测。

完整操作见[PRODUCT_OPERATIONS](PRODUCT_OPERATIONS.md)。

## 1. 最小 Embedding 配置

把 `examples/providers.embedding.json` 复制成 `providers.json`，修改模型、维度和服务地址：

```json
{
  "embedding": {
    "url": "https://模型网关/v1",
    "key": "env:EMBED_API_KEY",
    "model": "实际模型标识",
    "dimension": 1024,
    "revision": "由部署者声明的模型修订"
  }
}
```

URL为API根，适配器追加 `/embeddings`；文档输入是标题、标题链、块正文，query单独使用query_prefix。返回 `{data:[{index,embedding}],usage}`。维度、索引顺序、有限非零向量会检查。Secret只支持显式env:/file:，不是明文。

启动：

```bash
export EMBED_API_KEY='填写自己的凭据'
XRAG_DEMO_CONFIG="$PWD/providers.json" bash demo.sh
```

不要把凭据写进提交文件。配置后导入会真实调用模型；默认每个构建最多200次外部调用，适用于小样本Demo。模型换身份/前缀/维度，重新构建；旧模型数据不会被当成新模型可直接查询。

## 2. 可选专业后端

在配置中增加相应字段（不配置则不连接）：

```json
{
  "qdrant": {"url":"http://127.0.0.1:6333","key":"env:QDRANT_API_KEY"},
  "neo4j": {"url":"http://127.0.0.1:7474","database":"neo4j","user":"neo4j","password":"env:NEO4J_PASSWORD"}
}
```

Qdrant依赖embedding配置；Neo4j不依赖Embedding，默认可把明确标注写成关系。匿名本地Qdrant省略key。

- Qdrant 使用物理collection，不走可变alias。命名含origin hash、KB、模型配置hash；`xrag_demo_meta`保存描述。不得让其他工具写入这个命名空间。
- Neo4j 使用 `/db/{database}/query/v2`，需要目标版本支持Query API，不使用已弃用的旧事务端点。HTTP 202同时有errors也算失败。
- 图里不存正文；Server从SQLite读正文，direct从Qdrant点读正文。
- 本Demo不负责启动数据库，不隐式修改别人的知识库，不自动修复生产索引。
- 外部写入先于SQLite发布。Server按edition过滤；direct本来不读取发布元数据，可能见到staging/retired。
- 本轮无真实Qdrant/Neo4j实例，仅模拟协议测试；后端实际运行和所用版本由部署者联调。

## 3. Wiki 与重排

`chat` 字段同Endpoint，适配 `/chat/completions`，非任意厂商统一接口；Wiki默认采用“大纲→正文”两次固定生成调用，可显式single_pass。`rerank` 适配 `/rerank`，请求 `{model,query,documents,top_n}`，返回 `results:[{index,relevance_score}]`，供应商不符合时需增加适配。

Wiki按钮生成的是草稿，发布仍为单独动作。没有模型时不会生成“模拟结果”。

## 4. Client direct

Client配置在 `~/.xrag-demo/client.json`，具体结构见 `examples/client.direct.json`。复制实际物理集合名称，填写对应origin和模型声明（origin会从descriptor读取），指向**这个Demo写出的数据**。

```bash
./bin/xrag search semantic 日志 --config ./client.direct.json --profile direct --kb demo
./bin/xrag graph neighbors --name 设备A --config ./client.direct.json --profile direct --kb demo
```

Client直接使用后端身份，不继承XRAG Server Key。direct没有完整文件树、grep、长期历史存档或Server瞬时撤回。关键词为Qdrant文本过滤，不是BM25；无精确全集总数。只匹配当前physical_collection的模型，错误不回退到其他集合。

## 5. 独立抽取器

```json
{"extractor":["python3","-I","examples/extract_explicit.py"],"extractor_revision":"explicit-line-v1"}
```

支持在本进程宿主的工作目录运行；相对插件路径依工作目录解释，部署建议绝对路径。每个块一个进程，最多30秒和1MiB stdout；仅传PATH/LANG，不传模型或数据库凭据。生产级进程沙箱未实现。

请求 `{protocol:"xrag.extract/v1",id,chunk_id,text,revision}`，响应 `{id,result:{relations:[{from,to,rel,evidence}]}}`。插件只给结果，宿主负责写库。example只读取人明确写的 `关系: A | 使用 | B`，不是LLM抽取。默认的 `[[A]] --使用--> [[B]]` 不需要插件。

真实联调脚本与边界见[LIVE_INTEGRATION](LIVE_INTEGRATION.md)。

## 6. 参考资料

以下用于接口实现，不代表本轮外部联调完成：
- SQLite FTS5：https://www.sqlite.org/fts5.html
- Qdrant points：https://qdrant.tech/documentation/concepts/points/
- Neo4j Query API：https://neo4j.com/docs/query-api/current/query/
- MCP固定版本tools：https://modelcontextprotocol.io/specification/2025-06-18/server/tools
