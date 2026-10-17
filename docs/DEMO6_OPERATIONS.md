> **历史批次记录**：以下内容保留该 Demo 发布时的行为、命令和验证结论。当前操作请从 [文档总览](README.md) 进入，版本与已知问题见 [STATUS.md](STATUS.md)。

# Demo.6 操作说明：索引查询、自动维护、反馈到题集

版本 `0.7.0-demo.6`，使用**全新数据目录**。本版不转换旧程序的数据结构；检测到其他存储结构会返回 `STORAGE_VERSION_UNSUPPORTED`，不会把缺失的索引解释为“没有资料”。Demo.6 自身的重启、内容更新、固定引用和同版备份恢复仍然支持。

## 1. 新目录运行

```bash
bash demo.sh
# 浏览器 http://127.0.0.1:8765，使用终端显示的 Key
./bin/xrag profile add team --server http://127.0.0.1:8765 \
  --key-file "$PWD/.xrag-demo/admin.key" --kb demo
```

构建仍需 Go 1.23+、C 编译器、FTS5 SQLite 开发库、make。默认无需模型。Linux 包可运行 `bash run-linux.sh`；不在运行时自动安装数据库或下载模型。

## 2. 哪些查询不再载入整库正文

**Server 的 `search.keyword`、`files.tree` 在未指定 within 时，使用新的索引读取路径。**

1. 从 edition_catalog 读取当前已发布文件的身份、字段、标题和词典；不读取 edition.data 中的正文或向量。
2. 按实际保存的词典执行 FTS5，过滤条件在完整计数与排序前应用。
3. 保存有序结果的引用与元数据。第一页只读取本页的块正文，不把整个结果文本复制到缓存。
4. 翻页读取保存的引用，不重新搜索，不重新解析当前活动版本。
5. 文件范围句柄也只保存引用；后续动作消费范围时才加载选中的文件版本。

`files.read --path` 直接定位一个已发布文件，不先 Snapshot 整库。按 ref 阅读的含义不变。

```bash
./bin/xrag search keyword E203 --profile team --trace
./bin/xrag files tree --profile team
```

响应 strategy.execution 为 `indexed_metadata_then_page_hydration`。trace 区分 `indexed_scope`（元数据范围）、`index_match`（完整匹配数）、`result_window`（保存窗口）。

### 数量与边界

- `total` 是完整过滤范围上的计算；`per_file` 只限制选中结果。
- 保存窗口仍有上限，默认最多 20,000 项引用。超限时 `cache_complete=false`，不会把这部分当成全部；auto 不建立伪完整 handle，required 则明确报 `HANDLE_TOO_LARGE`。
- 当前页和后续页的正文来自原物化版。普通更新后仍能读取允许保留的旧版；明确撤回会阻止旧页返回。
- 元数据和引用仍随文件／匹配数量增长，本版不是恒定内存或无限规模搜索引擎。
- **语义、图、带 within 的复杂操作、`+ask`、direct 仍有范围 View／读取上限。**本轮没有宣称全部查询已经懒加载。
- 同题评测固定的完整 View 保持原路径；新索引性能与评测效果是不同验证对象。

## 3. 自动备份和操作记录维护

入口：设置与帮助 → 自动维护。配置初始关闭，只有显式启用后定期排队。

```bash
./bin/xrag maintenance policy --profile team
# 修改 examples/maintenance-policy.json 的 expected_revision，再保存
./bin/xrag maintenance policy --profile team \
  --data @examples/maintenance-policy.json
./bin/xrag maintenance records --profile team   # 只预览
./bin/xrag maintenance run --profile team \
  --data @examples/maintenance-run.json --confirm
./bin/xrag maintenance runs --profile team
```

配置字段：

| 字段 | 含义 |
|---|---|
| enabled | 自动计划开关，默认 false |
| backup_every_seconds | 0 关闭；60～604800 秒 |
| backup_keep | 保留维护流程创建的快照数，1～15，默认 7 |
| records_every_seconds | 0 关闭；60～604800 秒 |
| records_keep_days | 1～36500 天，默认 30 |
| batch_size | 每类记录每批 1～1000 条，默认 200 |

### 执行在哪儿

- API-only 的 `--workers off` 只保存配置和排队，不执行备份。
- 带内置执行器的 Server，或独立的 `xrag-server worker`，运行维护循环。
- 每次最多一个 running 维护任务；记录共享租约，过期执行标为失败，不静默重做一次结果未知的备份。
- 单次维护限时四分钟；持有最长十分钟租约。取消进程不会无限等候本机 SQLite 连接锁。
- 关闭计划取消未开始的自动任务，不取消已执行中的任务，也不取消明确提交的一次性请求。
- 自动整理有积压时，按当前相同配置继续分批；配置改变或关闭后停止继续排程。一次性请求只做一批。

### 哪些备份会被轮转

新快照成功保存后，才删除超过 backup_keep 的**维护流程快照**。原“数据管理 → 创建数据库快照”与 `backup create` 创建的手动快照不被此计划删除。

依然最多登记 20 份快照；手动快照占满空间时维护会失败，不能为了成功自动删掉人工留存的快照。

快照采用 SQLite `VACUUM INTO`，包含入库正文、知识、任务、配置引用和 Key 摘要。**它可能占用同一查询连接，不是零停顿备份。**它不包含来源目录、实际 Key、Qdrant、Neo4j。使用同版 restore 建立副本后，原维护计划与未完成任务关闭。

### 哪些记录会整理

- 已结束且超过保留期的任务详情、build_detail、trigger、已结算调用明细。
- 已结算 budget_call 先归并到按库、run、日期、操作的汇总，再删除明细；用量报表和每 run 额度继续包含这些汇总。
- sent / outcome_unknown / usage_unknown / cost_unknown 不当成零消耗删除。
- 运行中任务、知识条目、原文历史、反馈、人工题集、已采用的依据不由这个流程删除。
- 精简任务记录保留任务身份和幂等关系。重复同一请求返回 `JOB_EXPIRED`，不会创建第二份历史任务。
- 已过期的结果页、handle、查询摘要按批清理。任务年龄按完成时间，不按提交时间计算。

**这是在线操作记录维护，不是在线全文版 GC。**离线 prune 仍处理内容版；整个旧物理集合回收、外部联合备份／恢复仍未做。

## 4. 从反馈形成可运行题集

工作台路径：反馈处理 → 整理成测试问题 → 检索评测 → 确认问题与期望 → 选择问题 → 发布题集 → 运行比较。

```bash
./bin/xrag feedback test-case FEEDBACK_ID --profile team \
  --data @examples/feedback-test-case.json
./bin/xrag eval drafts --profile team
./bin/xrag eval approve DRAFT_ID --profile team \
  --data @examples/eval-approval.json
./bin/xrag eval from-drafts --profile team \
  --data @examples/eval-from-drafts.json
./bin/xrag eval export SET_ID --profile team --out NEW_FILE.json
./bin/xrag eval run SET_ID --profile team --wait
```

示例 ID、revision、路径需要替换成实际值；`eval export` 输出文件必须不存在。

### 不会自动发生的事情

- “有用”最多给一个期望路径建议，仍待维护者确认。
- “不相关”不等于整个查询应没有结果，不自动设置 expected_empty。
- 草稿不进入运行题集。发布只接受明确批准的本库问题。
- 更改草稿不会修改已发布题集；重新发布得到新的 set_id 和内容修订。
- 原反馈支撑资料被明确撤回后，相关草稿不再返回问题正文，不能批准或发布；由这些草稿形成的题集也不再提供。
- 普通原文更新不自动删除历史反馈，但实际运行时仍检查题集路径与锚点是否适用于当前测试范围。

### 调试集与留出集

Set 增加 `family` 和 `purpose`，purpose 为 development 或 holdout。不指定时按 default 系列／development 处理。

同系列中，去首尾空格、折叠空白并转小写后的相同问题，不能同时进入两种用途。此项检查是**字面规范化去重**，不是语义相似题检测；也不强制隐藏留出集内容。不同系列可独立管理。

## 5. 为什么没找到这份资料

工作台：检索评测 → 最近查询 → “为什么没找到这份资料？”

```bash
./bin/xrag trace diagnose QUERY_ID --profile team \
  --data @examples/query-diagnose.json
```

服务读取已经保存的 trace，再只读取指定的当前文件。返回：当前是否发布、锚点是否存在、当前字段是否符合原请求、记录页是否曾返回该文件、已保存阶段 ID 能观察到什么。

**不会重新搜索或调用模型。**

- 当前版本不代表执行当时版本。
- trace 没有存齐候选时，用 unknown，不推断“肯定被排除”。
- 没有返回全部页面时，“本页没返回”不等于“全部没有”。
- `within` 缺少可还原历史信息时明确说明，不拿当前范围代替原范围。

这是一次可使用的诊断入口，不是完整离线重放系统。实际重跑仍通过查询或评测显式执行。

## 6. 接口新增

| 路径 | 方法 |
|---|---|
| /v1/maintenance/policy | GET / PUT |
| /v1/maintenance/runs | GET / POST |
| /v1/maintenance/records | GET，预览 |
| /v1/feedback/{id}/test-case | POST |
| /v1/eval/drafts | GET |
| /v1/eval/drafts/{id}/approve | POST |
| /v1/eval/sets/from-drafts | POST |
| /v1/eval/sets/{id} | GET |
| /v1/queries/{id}/diagnose | POST |

维护操作限 Admin；题集整理限 Maintainer；所有题集和诊断仍使用实际知识库范围。MCP 查询／草稿／反馈工具集不扩成系统维护工具。

## 7. 本轮没有改变的边界

Server 构建、Client 读取；`+ask` 不生成答案；M6 不运行。真实 Qdrant／Neo4j／商业模型及第三方客户端联调依然未执行。本轮真实测试的是 SQLite、Go 进程、文件、HTTP、CLI、工作台操作以及现有受控适配测试，详情见 checks/REPORT.md。
