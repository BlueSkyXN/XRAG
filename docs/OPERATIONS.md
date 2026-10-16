> 当前版本为 Demo.5。本文件保留对应模块的原操作说明；新增行为或冲突以 [DEMO5_OPERATIONS.md](DEMO5_OPERATIONS.md) 与 [STATUS.md](STATUS.md) 为准。不要求旧软件迁移。

# 持续更新与效果比较（Demo.2引入，Demo.3保留）

本文件描述Demo.2引入并在Demo.3继续工作的接口。新增管理功能见[产品操作](PRODUCT_OPERATIONS.md)。默认不配置模型；`+ask` 是检索组合，不是回答生成或 Research。

> Demo.4继续保留本文既有命令；本轮新增能力与限制见[LIFECYCLE_AND_SOURCES](LIFECYCLE_AND_SOURCES.md)和[STATUS](STATUS.md)。

## 1. 启动与连接

```bash
bash demo.sh
# 另一个终端；仍在项目根目录
./bin/xrag profile add team --server http://127.0.0.1:8765 \
  --key-file "$PWD/.xrag-demo/admin.key" --kb demo
```

服务端登记的路径属于 Server 所在机器。远程 Client 上传资料时传文件内容；不能将用户电脑路径当作服务器路径。

## 2. 本地目录和 Git 来源

```bash
./bin/xrag source add --profile team --id handbook \
  --kind path --root /absolute/path/to/notes --mount handbook

./bin/xrag source add --profile team --id project \
  --kind git --root /absolute/path/to/repository --git-ref main --mount project

./bin/xrag source list --profile team
./bin/xrag build run demo --profile team --mode incremental --wait
./bin/xrag jobs details JOB_ID --profile team
```

把实际返回的任务编号填入 `JOB_ID`。管理参数 `--kb` 选择知识库，不会被错误地塞进来源/策略的 JSON。

Git：每次先解析 branch/tag/SHA 到一个 commit，再用该提交的完整文件树与 blob 读取。没有 checkout、不看未提交修改、不执行仓库 hooks、不取子模块、不读取符号链接。改名按移除旧路径和新增路径处理，已经签发的旧引用仍指向旧文件版。

`--root` 也可以是无用户名、密码、query 的公共 HTTPS Git URL。程序使用自己的 mirror cache，禁用交互登录；这一分支代码存在，但本轮只实际测试了本机 Git 仓库。SSH、HTTPS 私有仓凭据助手、Git LFS 内容展开未实现。私人仓可先在服务器上自行 clone，再登记为本机 Git 来源。

需要 git 命令的只有 Git 来源。普通 MD/TXT 上传、目录、查询不依赖 Git。

**读取上限**：每来源最多 5,000 个支持的文件、单文件 5 MiB、总正文 64 MiB。先完整读取成功才做缺席移除；任何读取/枚举失败、超限、分支不存在都不把它解释为空目录。不承诺文件系统目录瞬时快照；Git 才固定提交。Git 扫描为逐 blob 读取，尚未做大仓性能优化。

`full` 会为全部支持文件重新物化版；健康的模型输入缓存仍可复用。与正在排队的 incremental 合并时不丢 full 意图。所有触发当前都构建整个 KB，而不只构建通知来源；后台仍有同 KB 运行互斥。

## 3. 显式自动检查

### Git 定时检查

```bash
./bin/xrag source policy --profile team --id project \
  --data '{"every_seconds":60,"watch_seconds":0,"debounce_seconds":10}'
```

### 本地目录变化轮询

```bash
./bin/xrag source policy --profile team --id handbook \
  --data '{"every_seconds":0,"watch_seconds":5,"debounce_seconds":10}'
```

`every_seconds`：0 关闭；正值范围 10～604800 秒。`watch_seconds`：0 关闭；正值 2～3600 秒，仅 path/builtin。`debounce_seconds`：1～300，0 或省略采用 10 秒。

这是**读取文件指纹的轮询**，不是 fsnotify/inotify，也不是 cron 表达式。每次读取支持文件计算强指纹，能发现相同 size/mtime 的正文修改；因此频率越高，I/O 越多。来源逐个检查，慢来源会延迟其他来源，暂无分布式采集或并发扫描池。

配置默认全部关闭。Server 运行时负责检查并入队，不在检查器里构建或调用模型；开启 `--workers off` 后仍可入队，由外部同机 Worker 执行。只运行 Worker 没有来源检查器。与旧 `--scan-every` 共存，但不建议同时启用；旧参数每隔一段时间直接排整个库，本版按来源策略则先检查是否变化。

同一来源指纹未变化且已成功建设，不创建新任务；已排队/运行的相同指纹不反复创建 successor。新变化会合并到最多一个待执行任务。部分构建失败后，后续检查会按配置间隔再尝试；这不是精细指数退避计划，配置付费模型时应选择合理间隔。

```bash
# 关闭自动检查；若已有任务在执行，需单独取消
./bin/xrag source policy --profile team --id handbook \
  --data '{"every_seconds":0,"watch_seconds":0,"debounce_seconds":10}'
```

配置保存在数据库，重启后保留。关闭轮询不关闭已经登记的 Hook；需要禁用 Hook 将 `hook_secret_ref` 清空。来源地址/挂载在当前界面中不提供原地编辑，避免旧引用被重新解释。

## 4. 自定义签名 Hook v1

Hook **只通知重新检查来源**，不传资料内容。此协议不是 GitHub/GitLab 原生 Webhook；接原生通知需要额外适配/转发。

在 Server 启动前设置独立秘密值（示例为占位符，至少32字符）：

```bash
export XRAG_HOOK_SECRET='replace-with-your-own-random-secret-at-least-32-characters'
bash demo.sh
```

为来源启用，然后发送：

```bash
./bin/xrag source policy --profile team --id project \
  --data '{"every_seconds":0,"watch_seconds":0,"debounce_seconds":10,"hook_secret_ref":"env:XRAG_HOOK_SECRET"}'

python3 scripts/send_hook.py \
  --url http://127.0.0.1:8765/v1/hooks/demo/project \
  --secret-env XRAG_HOOK_SECRET --event commit-001 --reason 'docs changed'
```

发送端和 Server 都需要相同的值，`env:` 在各自机器分别解析，不会自动传递。也支持 `file:/absolute/path`，不要将值写入 Git。源策略中保存的是引用，普通 Key 不被透传。

协议：

```text
POST /v1/hooks/{kb}/{source}
Content-Type: application/json
X-XRAG-Timestamp: Unix秒
X-XRAG-Event: 1..128字符，无换行
X-XRAG-Signature: sha256=<HMAC-SHA256十六进制>

签名原文 = timestamp + "\n" + event + "\n" + 请求body原始字节
body = {"reason":"可选说明"}
```

接受当前时间前后300秒，body最大64KiB。同来源、相同 event 和同 body返回原任务；不同 body重用 event返回409。签名错误返回401。重新发送可以更新时间戳和签名，但 event/body应保持相同。返回202只代表入队，不代表构建完成。关闭 Server 时 Hook 不会替你启动服务。

## 5. 同题比较

界面「检索评测」可以载入内置合成题集，也可以导入自建 JSON。

```bash
./bin/xrag eval import --profile team --data @examples/eval-demo.json
./bin/xrag eval list --profile team
# SET_ID 用导入返回的 id；最多 100 题、5 种策略
./bin/xrag eval run SET_ID --profile team --wait --timeout 10m \
  --data '{"strategies":["keyword","ask","ask_graph"],"k":5}'
./bin/xrag eval report JOB_ID --profile team
```

默认 k=5，可设1～50。策略：keyword、semantic、ask、ask_graph、ask_rerank。graph/rerank/external 不采用现场 auto；external始终关闭，只有选定 graph/rerank策略才启用相应开关。纯语义或重排没有模型时在提交阶段拒绝。

在 Server 上，`ask` 使用宽松词法召回（OR、BM25），`keyword` 保持严格匹配（AND）和精确计数；不配置模型时，两者的结果也可能明显不同。报告仍记录实际 strategy。题集用于合成语料回归，业务效果需结合真实语料和自建题集评估。

每个正例必须有至少一个预期路径及可选 anchor；负例必须显式 `expected_empty=true`。未写 expected 不会自动当负例。重复目标拒绝。题集重新导入生成新 ID/内容修订，不原地修改旧报告所用题集。

运行前检查预期路径/原文锚点存在，失效题使任务报 `EVAL_DATASET_INVALID`，不跳过难题。一个任务中的所有策略使用固定文件版清单，并阻止同KB构建/知识发布并行。其他库对共享FTS全局词频的影响、远端模型变化或后端被外部改动，不能由这个锁冻结。精细排名对照应在停止其他写入的实例中执行。

**指标**：Hit@K、Recall@K、MRR按正例计算；重复文本块不会将同一目标多算。负例单独统计正确率。执行错误计入分母、不当成正确负例；有错误行任务为partial。p50/p95只是这一小题集本次耗时，不是吞吐压测。报告列“仅A命中/仅B命中”、逐题遗漏、实际策略和步骤。

报告保留 items身份与来源、过程摘要，不存完整候选原文。来源发生明确撤回时，旧报告不再返回；普通新版本发布仍可查看历史报告。没有图数据库或模型时不会虚构它们的效果。

当前评测复用固定10分钟任务执行上限，模型型评测真实发起付费调用。评测中的查询尚未接入完整token/金额账务，也未复用构建的每次调用200次限制；100题×5策略是最大动作数量，建议先用5～10题。客户端 `--wait` 超时不取消后台任务，可另查 jobs。

## 6. 实际过程与构建细节

```bash
./bin/xrag search keyword E203 --profile team --trace
./bin/xrag trace list --profile team
./bin/xrag trace show QUERY_ID --profile team
./bin/xrag jobs details JOB_ID --profile team
```

`--dry-run`/explain不执行；trace是实际执行后的信息。查看历史不会再次调用模型。查询历史最多2,000条、7天；过程最多30个步骤、每步200个ID、100个返回条目，不默认复制全文。它不是完整全路径重放，不能从没保存的候选推断“某文档肯定没有召回”。

构建详情提供来源、路径、变更/未变/移除/失败、前后edition、Git提交、触发原因及已有用量记录。接口最多返回1,000项文件详情、100条触发；暂无详情分页，API带上限说明，不把它称完整无限导出。

工作台「知识沉淀」新增编辑已有草稿；未发布修改不会覆盖上一发布正文。Wiki重新生成、来源写回和自动主题发现仍未实现。
