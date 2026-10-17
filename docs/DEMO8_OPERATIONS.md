> **历史批次记录**：以下内容保留该 Demo 发布时的行为、命令和验证结论。当前操作请从 [文档总览](README.md) 进入，版本与已知问题见 [STATUS.md](STATUS.md)。

# Demo.8 操作说明：直连、在线内容整理、投影恢复与委托调查

版本 `0.7.0-demo.8`，存储布局 `demo8`，只支持全新安装和同版快照恢复。本文件描述实际程序行为；真实第三方服务和模型的验证程度见 `checks/REPORT.md`，不要把受控端点测试理解为部署完成。

## 1. 启动与组件

```bash
bash demo.sh
# http://127.0.0.1:8765；使用终端显示的 Key
./bin/xrag profile add team --server http://127.0.0.1:8765 \
  --key-file "$PWD/.xrag-demo/admin.key" --kb demo
```

仍然是两个程序。`xrag-server serve --workers off` 提供接口但不执行后台任务；`xrag-server worker --data .xrag-demo` 在同机执行。Web、MCP 开关独立。默认不启动外部数据库、不下载模型。Server 使用 Unix 文件锁与系统 SQLite；Windows Server 原生支持未完成。Mac 源码未实机验证。

本轮增加的 Research 是单独任务入口，不改变 `+ask` 的固定检索含义。现有草稿、Wiki、写回、评测、备份、内容引用继续保留。

## 2. Direct 查询实际改变

### 本地执行端调用外部后端，不绕经 Server

- `search.keyword`：将支持的条件放入 Qdrant，读取匹配点的元数据，不先读取整个集合正文。它是 Qdrant 文本过滤，不冒充 BM25 排名。
- `search.semantic`：向固定的物理集合执行向量查询，保存最多 200 个候选身份；不为该路径 scroll 整库。
- 简单 `+ask`：合并语义排名与无相关性排名的文本匹配结果；未启用图、重排、external 且条件可下推时采用原生候选路径。
- 当前页才用具体点 ID 获取正文；后续页继续读取已保存身份，点丢失或身份改变明确报错。
- `files.read --ref` 只读取所指点。直连不按 path 伪装出完整文件；Neo4j-only 明确返回没有正文。
- 多 KB binding 各用自己的物理集合与模型。数据域一致要求保留；没有任意已有图/向量模式猜测。

Native 路径仅接收当前适配明确支持的条件（eq/in 等）。`within`、复杂过滤或组合条件走元数据执行路径，不静默丢弃过滤。元数据仍有 200,000 点上限；这不是恒定内存、无限容量或生产 SLA。旧的外部点若没有 `query_mentions`，应从当前发布资料重建，而不是把缺字段解释为没有实体。

Server 始终以自己的发布范围为准；direct 读取的是后端当时的数据，不继承 Server 的 active/撤回视图。现有 direct 配置仍需填写实际 origin、物理集合和正确模型描述，见 `examples/client-multi.json`。

## 3. 图路径按层读取邻接关系

对于 `graph.path` / `graph.neighbors`，从已到达的实体集合出发，逐层读取对应方向的关系支撑。Neo4j 请求在返回前同时限制：资料域、已发布或 within 选定的块、当前实体、方向。

主程序再次检查出处和文件版，不接受范围外关系。支撑总数最多 20,000，前沿最多 5,000 个实体，深度最多 5 层；超限明确失败。路径最终仍在 Core 中组装。这是邻接查询下推，不是任意图查询语言或把所有路径算法完全放入 Neo4j。

## 4. 在线内容版本整理

工作台：设置与帮助 → 自动维护。CLI：

```bash
./bin/xrag maintenance content --profile team  # 仅预览
./bin/xrag maintenance run --profile team \
  --data '{"content":true,"confirm":true}' --confirm
./bin/xrag maintenance runs --profile team
```

自动计划字段：

| 字段 | 默认与含义 |
|---|---|
| `enabled` | false；总计划开关 |
| `content_every_seconds` | 0；不开启自动内容整理 |
| `content_keep_days` | 30；退休版本至少保留多久 |
| `content_external` | false；是否明确允许按原绑定清理外部版产物 |
| `batch_size` | 与操作记录共享配置；内容整理每批最多 100 个扫描候选 |

API-only 只排队。维护 Worker 在每批处理时取得内容独占租约；普通查询、资料建设和任务执行持有共享租约。已有操作先完成，新的内容操作可能等待。进程不用停止，但不是无等待或无停顿；长调查会延后清理批次，超时可明确重提。

每次扫描保存推进游标，避免总在开头一批受保护版本上循环。当前发布内容、有效结果范围、已采用知识依据、反馈、题集、任务和保留窗口仍被保护。先处理该文件版登记的外部产物，再删除本地数据；远端失败保留本地与处理状态。未明确允许 external 时，不自动删除远端。

只协调同机且使用该资料目录租约的进程。不知道第三方 direct 客户端正在读取哪一版；外部退休数据的保留窗口需要覆盖实际使用期限。不是分布式事务。整向量集合回收仍用显式离线 `vector-gc`，不因在线内容批次而自动删库。

## 5. 从快照恢复 XRAG 管理的外部投影

这轮交付的是：**由 SQLite 已保存的正文、数值向量和关系支撑恢复 XRAG 管理的数据**，不再要求为了恢复而重新调用模型。它不是供应商完整实例备份，也不包含别的应用写入的数据。

```bash
# 必须使用新的、尚不存在的恢复目录
./bin/xrag-server restore --from snapshot.sqlite --data /绝对路径/新恢复目录 --confirm

# 编辑 examples/recovery-targets.json，只填实际专用恢复目标；不要填当前在用库
./bin/xrag-server recover-projections --data /绝对路径/新恢复目录 \
  --targets /绝对路径/recovery-targets.json

# 看过计划后明确执行
./bin/xrag-server recover-projections --data /绝对路径/新恢复目录 \
  --targets /绝对路径/recovery-targets.json --confirm
```

预览只读本地，不发模型或数据库请求。只有带同版恢复标记的副本允许执行。来源端不被改写。目标 Qdrant 物理集合须尚不存在，Neo4j 中本 authority 不能已有节点；恢复期间应禁止其他程序写入该目标。凭据使用目标的 `env:`/`file:` 引用，不包含在快照中。

先持久登记本次恢复身份再写远端；按文件版使用确定身份重写数据，保存已完成进度。发生失败保持副本不可提供服务，使用同一目标重提；完成后重复执行返回已有结果，不再次写入。原引用身份保留。最多处理 100,000 个可读物化版；排除撤回与未发布中间版。配置未提供某类目标，不假装恢复了该类数据库。

恢复后模型与自动动作仍默认停用；重新配置匹配的查询模型、数据源和执行器，再明确启动。已保存的向量不能反推出模型权重。没有保存成功的模型产物，不会因恢复而自动补全。

真实 Neo4j/Qdrant 现场恢复尚未运行；本轮验证实际 SQLite 快照/恢复与受控数据库接口，确实发送原已保存向量与关系，模型调用数为零。

## 6. Research：真正多步、只读的委托调查

API `POST /v1/research`。需要配置目标知识库的 Chat；缺模型在入队前报错。可以在工作台“委托调查”使用，或：

```bash
./bin/xrag research run --profile team --data @examples/research-task.json --wait
./bin/xrag jobs show JOB_ID --profile team
# 取消沿用 jobs cancel；关闭终端不是取消
./bin/xrag jobs cancel JOB_ID --profile team
```

输入为单个 `kb`、`goal`，可选 `path`、`max_steps`（默认8，上限16）、`max_seconds`（默认120，上限600）。目录限制形成固定文件版范围，不自动扩大。排队期限24小时；首次领取写入绝对执行期限，重新领取不重置。相同幂等键与请求沿用同一任务。

```text
任务 → 固定范围 → 模型选择查询 → 读取结果 → 决定补查或结束
                                         ↓
                          发现 / 类型化依据 / 未解决部分
```

模型通过严格 JSON 返回动作或结束信息，调用同一 Core。允许已公布的搜索、grep、读取、图查询、上下文与固定组合；不执行 Shell、任意 SQL、业务写操作、远程 URL、递归 Agent，也不扩张为通用工作流。

每次返回最多10项、正文最多5000字符；单个工具结果与全部调查上下文均有字节上限。重复相同动作会停止。终止原因包含 completed、completed_with_gaps、step_budget、no_new_action 和明确错误。当前不透明续跑；断电/中断后的状态按现有任务系统说明失败，重新提交是新的明确操作。

模型步骤和查询中的付费调用共享同一个任务预算记录。未知调用不按零消耗结算。预算不是供应商最终账单，也无法取消已发出的远端请求。

### 依据规则

- Search 排名与摘录是候选；文档结论应通过实际 Read 登记依据。
- 关键词/grep 的完整统计可以登记 aggregate；其单位是文件/块/匹配，不自动成为业务客户数。
- 图路径/关系直接登记各自的结构化依据，无需硬找一篇所谓原文。
- 模型只能引用本任务登记的 e1/e2 等 ID；未登记引用被拒绝。
- 数值结论使用 `{n}` 绑定 exact 且完整的 `total`；程序读取数值与单位渲染，模型不再复制37成73。
- 成功或有明确缺口的最终结果保留被引用结果至少7天，返回 `references_expire_at`；普通更新仍可读历史，明确撤回使调查正文与依据停止返回。
- 程序检查结构、范围与数字；不能因此断言模型对关系、否定和业务因果的解释一定正确。

本次真实执行了任务队列、查询、读取、数字渲染与取消/失效行为；规划用受控 Chat 响应，不是实际商业模型效果。

## 7. Recipe：不用规划模型的有限检索配方

API `POST /v1/recipes/run`，示例：

```bash
./bin/xrag recipe run --profile team --data @examples/recipe-run.json --wait
./bin/xrag recipe run --profile team --recipe examples/recipe.yaml \
  --data '{"goal":"E203"}' --wait
```

配方 `xrag.recipe/v1` 含1–15个明确只读步骤。`$input` 表示输入任务文字；`$last.ref` 读取上一步第一项引用，前一步无结果就报错，不猜测资料。YAML 由 CLI 使用 Python3/PyYAML 转换，拒绝 aliases、重复键等；服务器只接受对应 JSON，不需要 Python。JSON 配方不要求 YAML 依赖。

配方不需要规划模型；选择 semantic 或 rerank 等动作仍需要实际模型。结果是操作记录和依据，`stop_reason=recipe_completed`，不生成一段假调查结论。没有任意表达式、循环、分支、动态代码或多 Agent。

## 8. MCP 与程序内使用

原16项查询、2项显式写工具保持不变。增加可选 `research` 工具组：research_submit、recipe_run、research_status、research_cancel。

```bash
./bin/xrag mcp --profile team --toolsets core,graph,files,utility,research
```

HTTP 可选 `/mcp?toolsets=research`。提交返回真实job，状态和取消使用显式工具；这是 XRAG 业务任务工具，不宣称实现了 MCP Tasks 协议。Research 与 Recipe 仅在 server 模式开放。公共 Go `research.Runner` 接口供进程内使用，独立于 HTTP；持久任务、引用延长等由宿主实现。

仍是已声明的 MCP tools 子集，不是 OAuth/所有可选协议的全实现，没有第三方商业客户端实机结果。

## 9. 新增入口摘要

| 入口 | 用途 |
|---|---|
| POST /v1/research | 提交调查 |
| POST /v1/recipes/run | 执行配方 |
| POST /v1/tasks/status | 同调用者范围内读取任务 |
| POST /v1/tasks/cancel | 请求取消 |
| GET /v1/maintenance/content | 内容版预览 |
| POST /v1/maintenance/runs | content=true显式运行一批 |
| xrag-server recover-projections | 恢复副本的已存模型/图产物 |

完整执行记录在本轮报告。没有旧版迁移或凭据自动搬迁。
