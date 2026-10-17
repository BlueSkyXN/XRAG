> **历史批次记录**：以下内容保留该 Demo 发布时的行为、命令和验证结论。当前操作请从 [文档总览](README.md) 进入，版本与已知问题见 [STATUS.md](STATUS.md)。

# Demo.7 操作：组合查询、退休集合与原生 Git 通知

版本 `0.7.0-demo.7`。全新数据目录，存储标记 demo7；不转换旧软件数据。当前版本自己的文件更新、历史引用和备份恢复仍支持。`+ask` 是固定检索组合；Recipe/Research 运行器继续不在 M1–M5 之内。

## 1. 启动

```bash
bash demo.sh
# http://127.0.0.1:8765，输入本机终端显示的 API Key
./bin/xrag profile add team --server http://127.0.0.1:8765 \
  --key-file "$PWD/.xrag-demo/admin.key" --kb demo
```

Linux 预编译包运行 `bash run-linux.sh`。Mac 使用源码在本机编译；本轮未做 macOS 实机测试。需要 Go 1.23+、C 编译器、含 FTS5 的 SQLite 开发库和 make。模型、Qdrant、Neo4j 均不自动下载或启动。

## 2. 查询：从整库正文改为查询用元数据

Demo.6 已有 keyword/tree 索引路径；本轮把 Server 的语义、图、+ask、scope.expand，以及带 within 的非 grep 查询接上共享的 compact 查询路径。QueryView 持有文件/块身份、过滤字段、实体提及和关系支撑，不持有全库正文和向量。数据在文件 Stage 时同事务准备，仍以文件 active 指针决定查询版本。

```text
确定已发布文件版与范围
          ↓
读取查询用元数据，不载入全库正文/向量
          ↓
关键词索引 / 向量召回 / 图支撑与邻接
          ↓
组合候选，必要时读取候选正文做 rerank
          ↓
只为当前结果页读取原版正文
```

- 本地 SQLite 精确余弦：逐个读取选定块的向量，采用 Top-200 堆；仍扫描范围内全部可用向量，只是不把它们积存在 Go 数组里。模型调用在 SQLite 读事务之外。
- Qdrant 查询：沿用物理集合和固定模型配置，发送选定的块范围；不要求把所有向量读回 Go。真实后端仍未联调。
- 图遍历：先限制关系的支撑文件/块，再构建邻接表。原始支撑数量全局上限 20,000，不再只有每次 Neo4j 批请求的局部限制；超过时明确失败，不声称图已穷尽。
- +ask：最多 200 个词法候选；重排前最多准备 200 个候选正文。不同数据库分数仍不直接相加。固定范围内计数与候选集合计数继续分开。
- within 保存固定引用；后续图查询仍受原支撑范围约束。普通更新不把旧引用改指新正文；明确撤回会阻止旧页和句柄继续返回。
- 缓存默认保存引用与结构，不重复保存候选原文。下一页读取旧文件物化版，不重新召回或重新调用模型。
- 自定义 Semantic 未显式实现 `CompactSemantic` 时保留完整 View 调用；这是一种明确的扩展能力协商，不以空正文强行破坏接口。

`strategy.execution=metadata_projection_then_page_hydration`；trace 有 `query_projection` 等实际步骤。Demo.6 的无 within keyword/tree 继续显示 `indexed_metadata_then_page_hydration`。

```bash
./bin/xrag search semantic "日志" --profile team --trace  # 需 Embedding
./bin/xrag +ask "组件B" --profile team --graph --trace
./bin/xrag graph path --from 设备A --to 日志事件 --profile team
```

### 没有被这次优化消除的限制

元数据仍随资料数量增长（本路径最多 200,000 块、50,000 文件）；保存窗口仍最多 20,000 项。图支撑包含出处摘录，不等于零文本。`+context` 为维持已有 Wiki 子串匹配，会读取选定范围内 Wiki 正文。全文 grep、固定完整 View 的评测、自定义适配器和 direct 的路径未全面改写；direct 仍有界 scroll。精确向量扫描是 O(N) CPU，而且会占用 SQLite 读取连接，不是 ANN 引擎或恒定内存承诺。尚无生产容量/SLA。

本轮实验的三次中位数及全部原始记录见 RESOURCE_PROBE7.md；它用固定合成向量和受控查询端点，不证明语义效果。

## 3. 离线回收整个退休向量集合

用途：完成旧文件版及外部点清理之后，删除本资料域曾实际登记、已经空置且不再被使用的 Qdrant 物理集合。

```bash
# 先停止本资料域的全部 Server 和 Worker，并处理/取消未完成任务
./bin/xrag-server prune --data /绝对路径/数据目录 --external --keep-days 30
# 看过预览后才加 --confirm 清理符合条件的旧文件版及外部点

# 整集合回收：默认只读本地登记，不连 Qdrant，不删除
./bin/xrag-server vector-gc --data /绝对路径/数据目录 --keep-days 30
# 决定执行后：
./bin/xrag-server vector-gc --data /绝对路径/数据目录 --keep-days 30 --confirm
```

如果运行配置平时使用 `--config providers.json`，这两条离线命令也必须传同一配置，确保“当前选中的模型”判断正确。按 KB 保存的配置直接从资料库读取。不要把维护命令指向空目录。

处理条件：

| 条件 | 行为 |
|---|---|
| 仍有任一 active/历史 edition 持有 | 不删集合；旧文件版先由 prune 处理自己的引用依赖 |
| 当前默认/KB 配置仍选择这个实例与模型 | 不删，即使尚无文件 |
| 最后一个文件版持有关系刚解除 | 此时开始集合退休保留期；不是模型文件创建时间 |
| 本地保留期未到 | 不连接后端 |
| 存在 queued/running 构建任务 | 拒绝执行本次回收 |
| 集合由别名指向 | 拒绝，不替其他调用者删别名 |
| 集合仍有一个或更多点 | 拒绝，不执行清空或删除；先查文件版清理结果 |
| 描述中的资料域/KB/模型/物理名不匹配 | 拒绝，不处理外来集合 |
| 以上成立，精确 count 为 0 | 删除该物理集合，确认消失，再清理 descriptor 并登记完成 |

采用原始后端绑定读取，凭据仍是引用，不把当前另一数据库的地址套到旧集合上。响应失败保留处理状态；集合已删但 descriptor 步骤失败，再次执行可以完成剩余部分。返回 JSON 包括 decision、removed、remote_checked，默认预览不会发网络请求。

**这是明确调用的离线整集合回收，不是自动在线 GC。**不删除未登记的孤儿/外来集合，不替直连客户端创造租约，不与 Qdrant 实现跨库事务；执行期间应保证本资料域没有不遵守本机锁的写入者或需要继续读取该退休代号的外部客户端。预览结果不承诺稍后远端一定满足条件。数据备份仍应独立保留。

本轮修正已有 `PruneExternal` 一处错误：DeleteEdition 接受 KB，但原调用传了物理集合名，导致再次拼接集合前缀。增加了精确请求路径回归。

## 4. GitHub、GitLab、Gitea 推送通知

入口仍为 `POST /v1/hooks/{kb}/{source_id}`。只在 Git 来源上启用；Git 克隆/读取身份与 Hook 密钥是两种配置。默认仍为 XRAG 自定义协议，新协议必须显式选择。

工作台：资料与构建 → Git 来源卡片 → **Git 推送通知**。或者：

```bash
# 在 Server 环境中设置足够长的随机密钥（至少32字符）；不要把真实值放进Git。
# 编辑 examples/source-policy-github.json 的仓库名/ref/密钥引用
./bin/xrag source policy --profile team --id YOUR_SOURCE \
  --data @examples/source-policy-github.json
```

字段：`hook_provider`=`github/gitlab/gitea/custom`；`hook_repository` 是大小写精确匹配的 `namespace/name`；`hook_ref` 是精确的 `refs/heads/main` 或 `refs/tags/...`；`hook_secret_ref` 为 Server 能解析的 env/file 引用。保存设置不会创建构建。

| 协议 | 接收与检查 |
|---|---|
| GitHub | application/json；原始字节 HMAC-SHA256；X-Hub-Signature-256 带 sha256= 前缀；X-GitHub-Delivery 去重；push，ping 不构建 |
| GitLab | X-Gitlab-Token 常量时间比较；Push Hook/Tag Push Hook 与 object_kind 一致；Idempotency-Key（缺失时 Webhook UUID）去重 |
| Gitea | 原始字节 HMAC-SHA256；X-Gitea-Signature 为无前缀 hex；Delivery 去重；push，ping 不构建 |

仓库不匹配拒绝；未选分支/其他事件只返回 ignored。同交付 ID 和相同消息返回原任务，改消息复用 ID 则冲突。普通通知不带统一时间戳，本接收器没有伪造“所有厂商都验证时间窗口”的承诺。GitLab Token 和远程所有通知应经部署者的 HTTPS 入口传输。

**通知只安排按已登记来源重新读取完整文件树。**消息中的 clone URL、文件删除列表、提交说明不能替换来源配置或执行命令。API-only 仍只入队；Worker 启动后才构建。来源停用后不接收构建通知。

本轮实际跑了本地 HTTP 请求＋本机 Git＋Worker。请求格式按厂商规范生成，但没有让 github.com/gitlab.com 或实际 Gitea 安装投递到此环境。公网可达性、反向代理和真实仓库凭据另测。

技术依据（协议不是由示例数据猜出的）：
- https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries
- https://docs.gitlab.com/api/project_webhooks/
- https://docs.gitlab.com/user/project/integrations/webhook_events/
- https://docs.gitea.com/usage/repository/webhooks
- https://api.qdrant.tech/api-reference/collections/delete-collection

## 5. 未完成项保持明确

真实 Qdrant/Neo4j/商业模型/S3/WeKnora 联合运行；direct 懒加载和完整图路径下推；在线内容版 GC 与外部联合备份恢复；第三方 MCP 客户端实机和完整协议/OAuth；Mac/Windows 原生运行。它们都不因这次新增功能变成“已完成”。
