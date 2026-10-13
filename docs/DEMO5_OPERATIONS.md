# Demo.5 实际操作与数据行为

**0.7.0-demo.5；全新数据目录。**本章描述代码已提供的操作，验证程度见 checks/REPORT.md。默认没有模型或远程库。

## 1. 知识库处理配置

`GET /v1/kbs/{kb}/settings` 读取，`PUT` 传 `{expected_revision,settings}`。CLI `xrag kb settings KB --profile team [--data @file]`。工作台“设置与帮助 → 知识库配置”提供表单。

- 未设置 providers：继承启动默认；设置对象：本库完整模型配置，而不是逐字段隐式混合。改动模型地址和凭据由 Admin 执行；Maintainer 可修改词典、字段、预算等而保留已有模型。
- 现有资料按生成时的模型身份查询；保存新模型不让旧资料立刻使用新查询向量。
- `vectors_only` 读取发布视图中的原文与块，不扫描目录；新向量准备完成后一次发布选定 KB 的物化结果。失败保留旧版。兼容发现别名更新失败只报告，不把已经提交的内容说成失败。
- `graph_only` 保留已存向量并复用对应模型配置，按新图范围/抽取器生成图表示；不重复调用 Embedding。
- `full` 重新解析当前来源，健康模型缓存可复用。`incremental` 处理新内容、配置变化、失败投影等 dirty 状态。
- 已配置向量/图可以显式 optional；按块失败状态记录、基础正文可发布。精确完整查询不能把缺失投影当作全部正常。

示例 `kb-settings.json` / `kb-models.json` 的 revision=0 仅用于尚未保存过的库；使用前读取实际 revision。运行中任务完成后再修改其执行配置。

## 2. 文档与图

默认轻量解析无外部运行依赖。需要 CommonMark/YAML 时登记 `plugins/commonmark/parse.py`，选择 parser_plugin。该插件真实使用 markdown-it-py 与 PyYAML；不以手写正则冒称完整 YAML。它拒绝 YAML aliases/重复或非字符串键，以限制展开歧义；不代表支持任意 Python YAML 对象。

模型输入与源码摘要分开：NFC规范化不会导致未改来源反复重建。文档位置为规范化正文中的 Unicode code point。大块按上限拆分，不给生成表头伪造连续来源区间。

字段 `string/number/boolean/date`；date 要求 YYYY-MM-DD，number 不接受 NaN/Inf。字段不满足登记类型时文件保持旧发布，不把错误值当缺失后仍宣称统计完整。

领域词典每行一个词，进入当前文档版的词项；它不是训练模型或保证业务相关性。分块上限100–20000字符。

图模式：
- explicit：`[[A]] --使用--> [[B]]`；
- llm：调用本库 Graph.Model，缺省可用本库 Chat；提取实体、类型、关系和原文 evidence；
- plugin：选启动时登记的 JSONL 抽取器。

只对 include/exclude 允许的路径建图。可限定实体和关系类型，重复支撑 ID 幂等。LLM evidence 的子串检查仅证明引用片段存在，不证明肯否、因果或条件被模型正确理解。当前实体按名字/KB识别仍有同名合并限制，不等于完整本体编辑器。

注册插件只是保存入口，不执行。`/v1/system` 为维护者列插件 ID/类别/版本，不暴露命令；本库选择 ID。同一实现可供多库以不同设置调用。当前子进程按调用启动、限时限输出后结束，不提供沙箱、常驻进程池或动态安装商店。

## 3. 查询与外部搜索

`scope.expand` 扩展来源集合，但请求里的 filters 仍作用于返回范围。`files.grep` 按完整已发布原文匹配，允许跨块；within 非连续范围不会拼接成不存在的文本。grep total 单位为 matches，neighbors total 单位 relations，support_count 单独表示支撑块数。

关键词支持 per_file，但 total 仍针对原查询完整范围。summary_ref 固定本次 total 与定义；返回十条不是总量十条。Discover 先读元数据，不先加载整库正文；其他复杂查询仍采用有上限的 View，尚非大型语料懒加载引擎。

`providers.external[]` 是具名 peer 配置，当前适配 **XRAG 同协议** `/v1/ask` 与 `/v1/files/read`，不是任意外部 API，也不是 WeKnora 实时接口。WeKnora 的另一条来源路线是已实现的解析文本导入。

```bash
./bin/xrag kb settings demo --profile team --data @examples/kb-external.json
./bin/xrag search external 日志 --profile team
./bin/xrag +ask 日志 --profile team --data '{"external":true}'
```

同次 +ask 的 remote 结果带上游身份与本地绑定修订；读取时不把本地 Key 发给上游，使用绑定自己的凭据。改绑定地址或 namespace 后旧引用明确拒绝，不猜测另一个实例。同一实现两个 ID 实例可以并存；跨 KB 同名 ID 对应不同地址时拒绝歧义。

目前 local within 句柄不能解释成上游范围，external 带 within 明确拒绝。上游返回部分执行也不假装完整成功。外部返回原文不会自动导入，当前不支持把不受本库管理的 remote ref 直接发布为本库长期知识依据。

## 4. 类型化依据与 Wiki

关键词 `summary_ref` 代表本次聚合，path/relation 项自己的 ref 代表结构结果。提交知识条目直接使用这些 ref，无需找一篇不相关原文充作依据。

采用时保存不可变结构、原查询、资料依赖、适用范围及显示完整性；归档随知识修订拥有，临时结果过期后仍可读取。只允许有权且同KB内容；来源撤回使相关派生依据停止返回，普通新版本只形成历史。保守集合撤回epoch可能使整个依赖集合的归档失效，不承诺精细到任意记录的全局依赖追踪。

新增 `applies_to`、`not_applies_to` 字符串，工作台和CLI均可设置。当前有据知识写入仍要求明确发布，不自动训练或修改排名。

Wiki 固定大纲→正文流程使用配置的词法/语义/图检索；范围在召回前限定原始资料。默认证据排除全部 managed 知识/Wiki，防止自引用；这比允许有出处的 managed 条目更保守。最多40块/40000字符，只说选中资料综述，不声称遍历全部事实。需要模型但未配置时拒绝。

Wiki 中明确的 `[[主题]]` 链接在发布构建时解析唯一的已发布 Wiki，保存 origin=wiki / links_to，不当业务因果关系。`+context` 返回相关发布Wiki组。未解析/有歧义的目标不强行建边。目标标题变化后需要重建相应引用页来刷新链接。

## 5. MCP 和 Client

16个查询动作与2个 opt-in write 动作共用结构。远程 `/mcp?toolsets=core,utility,write` 或 stdio `--toolsets ...write`。`knowledge_submit` 只建草稿；`feedback_submit` 只保存反馈。Reader不能写；不因工具开关提升角色。

MCP 声明仍是2025-06-18 tools子集。没有官方SDK、OAuth、资源/提示/Tasks或流式SSE。协议里有字段不意味着第三方客户端已经联调；不支持的协议行为不假装支持。

Client direct 支持 `bindings[]` 对多个 KB 分别配置 Qdrant/Neo4j/模型；相同资料域是目前限制，不做跨资料域自动路由。只配 Neo4j 时可查图和标识，但块无文本时 `text_available=false` / TEXT_UNAVAILABLE。直接模式的数据格式仍要求XRAG布局和已知origin。

Direct 对每个绑定固定物理集合和模型描述；后端实时/staging内容不具有Server的发布/撤回视图。当前仍读取有上限的元数据/点集合，不是无限规模分页图查询器。示例 `client-multi.json` 必须填写实际生成的physical_collection/origin_id。

## 6. 模型额度

每 KB 的 budget 可限定每逻辑请求/任务调用数、Token和微货币单位；UTC日期聚合。一个任务中的各模型步骤共享 run_id，重复网络尝试分别记call。支付模型的 query/build/eval/wiki 路径都通过预留与结算；上游独立检索服务自己的内部模型费不包含在本地账务里。

发送前输入按序列化UTF8字节加配置输出上限作保守估算，不声称精确tokenizer。配置金额上限必须提供该操作 rates；没有价格拒绝该受限调用。收到 usage 再结算；响应不明继续保留原预留，usage不明不计零。上游实际使用量高于预留时记真实用量并阻止后续调用，不能撤销已经产生的费用。

operation 价格键为 `embedding`、`chat`、`rerank`（以实际用量表为准）；客户端使用 `xrag usage show` 或工作台查看真实记录。模型名称相同不证明权重固定，查询仍依赖部署者正确登记model revision。

## 7. 来源与日常管理

KB可改展示名、删除（slug保留）；不把重名当同库复活。来源可编辑include/exclude/disabled，root/mount/kind保持不可变，需要改绑定用新ID。停用停止检查与Hook、保留现有发布；移除来源让当前资料退出查询，允许的历史引用继续读取。操作不修改原来源文件。

Cron是五个数字字段：列表、区间、步长＋IANA时区，不支持@daily等宏。Linux使用实际inotify，其他OS采用明确周期兜底；网络挂载仍需要兜底。原秒数检查与轮询继续可选。Hook目前仍是XRAG自定义签名格式，没有冒称直接支持各厂商原生回调。

## 8. 写回与清理

S3写回：登记s3目标，SigV4＋GET＋条件PUT；首次If-None-Match、更新If-Match。使用实际服务前应确认支持这些条件，不支持就失败，不退回无条件覆盖。公开签名fixture和受控HTTP不代表真实S3/MinIO部署。

Git目标push必须显式true＋remote_url，指定xrag/*分支，以已知远端SHA进行条件更新；从不默默强制覆盖未知远端。已验证本机bare remote，未联调公网/私有仓凭据；无任意SSH凭据助手管理。

目标auto_sync=true是显式订阅。只有以后发布成功的修订在发布事务写outbox，Worker排幂等sync任务。登记或草稿编辑不会执行；失败保留已发布内容和失败任务，显式重试，不无限重试。没有定时重写远端一切，也不会自动抹除已送出的副本。

`prune --external`先预览并要求离线锁。按曾记录的KB/edition/物理集合清理外部，再删除本地版。旧外部凭据不可用会保留本地版并报告错误。没有整个向量集合自动删除、完整在线GC、供应商快照集群或自动远程备份承诺。
