> **历史归档**：本文保留原批次说明与当时验证记录。当前使用请阅读 [文档总览](../../README.md) 和 [当前状态](../../STATUS.md)。文中 checks 等报告路径可能仅存在于原执行环境。

# XRAG 0.7.0-demo.2

**一个实际可运行的 Go Demo：Server 建设 MD/TXT 知识，Client 查询。**

它不是 V0.7 全部功能的完成版。先试验“资料导入 → 文件级发布 → 查与读 → 有出处的关系 → 知识草稿与发布”。`+ask` 只组合检索结果，不生成回答；通用 Research/Recipe 执行器未实现。


## 本轮增量：资料持续更新，检索可以比较

在原Demo上新增了Git固定提交读取、每来源定时检查/目录轮询、自定义签名Hook、小题集对比评测和实际过程查询。工作台可以登记来源、调整自动更新、导入题集和查看逐题结果。

**先运行 `bash demo.sh`，再打开“资料与构建”和“检索评测”。不配置任何模型也能体验新增功能。**

[新功能详细操作](../../OPERATIONS.md) · [从demo.1升级](UPGRADE.md) · [变更记录](CHANGELOG.md) · 真实检查结果（原路径 `checks/REPORT.md`，未归档）

Git源需要本机安装git；普通资料不需要。目录变化是显式轮询，不是OS事件监听。Qdrant、Neo4j、模型及公共HTTPS Git远程没有真实实例联调结论。

## 1. 最快启动

解压进入本目录，运行：

```bash
bash demo.sh
```

脚本本机编译两个程序，初始化独立的 `.xrag-demo/`，复制 7 份**合成**示例资料，然后启动 Server 并提交一次构建。**不下载 Go 模块、不下载模型、不启动 Qdrant/Neo4j、不修改系统设置。**

浏览器打开 **http://127.0.0.1:8765**，输入终端显示的 API Key。Ctrl+C 停止；资料仍保存在 `.xrag-demo/`。再次运行不会覆盖已有初始化数据。

### 本机构建条件

- Go 1.23 或更新，`go version` 可用。
- C 编译器与 SQLite 开发头文件／库，SQLite 含 FTS5。
- `make`。测试插件示例需要 Python 3；默认程序启动不依赖 Python。
- macOS：可从终端运行 `bash demo.sh`，也提供 `启动Demo.command`。需要 Xcode Command Line Tools；如系统未安装，先自行执行 `xcode-select --install`。**本轮未在 macOS 实机运行。**
- Linux Debian/Ubuntu：需要 Go、`build-essential` 和 `libsqlite3-dev`。由部署者按系统方式安装；脚本不自动 sudo 或安装。
- Windows Server 原生构建未支持（同机实例锁使用 Unix flock），可在 Linux/WSL 环境本机编译。

另有 Linux amd64 程序包，运行其中的 `bash run-linux.sh` 可跳过 Go 编译。预编译版在 Debian 13、系统 SQLite 3.46.1 验证；依赖系统 `libsqlite3` 与 glibc，在不同 Linux 发行版遇到动态库问题请用源码本机编译。

## 2. 先体验这五件事

| 体验 | 操作 | 实際行为 |
|---|---|---|
| 关键词与原文 | 检索 `E203`，点击读取原文 | FTS5 查询；引用固定原文件版，不是前端假数据 |
| 总数与分页 | 选择“关键词”，搜索 `分页样本记录` | 精确总数 37；每页 10，依次 10/10/10/7 |
| 关系路径 | 关系探索：设备A → 日志事件，沿实际方向 | 返回两段关系与实际出处；关系来自显式标注，不是模型猜测 |
| 限定来源 | 资料页选择 F1“只查此文件”，再查设备A关系 | F1仅提及设备A/组件B，不会把F2提供的关系带进来；清除范围后可查到 |
| 知识沉淀 | 检索结果点“用于草稿”，填写经验并保存 | 草稿不可搜索；明确发布且构建成功后才进入检索 |

自己的资料：在“资料与构建”上传 MD/TXT。上传后提交后台任务，成功后在 `uploads/` 下出现。也可登记服务器本机目录（下面有命令），只读取、不修改原目录。

**仅有文本时不会假装具备语义检索。**无模型仍可关键词、文件阅读、精确总数、手工关系、知识草稿与发布。Wiki 生成未配置 Chat 时明确拒绝，不返回假文章。

## 3. Server 与 Client

```bash
# 在另一个终端，当前目录仍为解压目录：
./bin/xrag profile add team \
  --server http://127.0.0.1:8765 \
  --key-file .xrag-demo/admin.key --kb demo

./bin/xrag search keyword E203 --profile team
./bin/xrag +ask "组件B" --profile team --graph --trace
./bin/xrag graph path --from 设备A --to 日志事件 --direction out --profile team
./bin/xrag files tree --profile team
./bin/xrag files read --ref '从返回结果复制 xr1: 引用' --profile team

# 只在本地扫描，不运行 Server、不建立本地知识索引：
./bin/xrag search keyword 日志 --local ./examples/docs --kb demo
./bin/xrag files tree --local ./examples/docs --kb demo

# 只解释请求，不进行网络或模型调用：
./bin/xrag search keyword E203 --profile team --dry-run
```

Client 配置是 JSON：`~/.xrag-demo/client.json`。`profile add` 会保存具名连接，凭据存引用，不写明文。只查询远程服务不创建本地 SQLite。每次调用选一个 profile；local/direct 不执行资料管理。

**上述命令是 Demo 实际接口；不是 V0.7 设计中所有命令的兼容承诺。**更多完整参数可用 `--data @request.json`；`./bin/xrag schema` 列实际动作，`contracts/` 提供对应 Schema。默认 JSON，`--format table` 可用于人工阅读。

## 4. 接入自己的资料

```bash
./bin/xrag source add --profile team --kb demo \
  --id my-docs --root /绝对路径/我的资料 --mount mydocs
./bin/xrag build run demo --profile team --wait

# 上传文件字节，不把客户端路径当成服务器路径：
./bin/xrag source upload ./notes.md --profile team --kb demo --wait
```

知识库创建：`./bin/xrag kb create my-kb --name 我的知识库 --profile team`。

同一来源每次完整枚举，取到全部目录后才将缺席文件移出当前视图。扫描失败保留原发布文件。文件正文或字段变化产生新 edition，模型缓存按实际输入复用。**旧版本默认保留到整个演示数据目录被显式清理；Demo 尚无完整 GC，磁盘会增长。**不要把它用于无上限持续导入。

普通来源移除保留旧引用；明确撤回 `files withdraw --path ... --confirm` 会阻止历史引用，派生知识也停止返回。撤回在本 Demo 不提供恢复原路径的操作。

## 5. 独立运行执行器

V0.7 要求两个程序。Server 默认含后台执行器；需要分开时：

```bash
# 终端 A：API/Web/MCP，不执行构建
./bin/xrag-server serve --data .xrag-demo --workers off
# 终端 B：同机后台执行器
./bin/xrag-server worker --data .xrag-demo
```

停掉 worker，已发布资料继续可查；新任务排队。`--scan-every 60s` 可以显式启用定期完整扫描。新增的每来源策略优先使用工作台或 `source policy`；目录使用轮询，不是 fsnotify。新增自定义签名 Hook，但不是 Git/OSS 厂商原生通知格式。详见 OPERATIONS。

## 6. 模型、Qdrant、Neo4j 与插件

见 [可选连接说明](../../CONNECTIONS.md)。只配置需要的字段，启动时用：

```bash
XRAG_DEMO_CONFIG="$PWD/providers.json" bash demo.sh
```

- 配置 Embedding：保存真实返回向量。无 Qdrant 时以 SQLite 保存的向量做精确余弦检索。
- 配置 Qdrant：真实 REST 读写适配；按 origin/KB/模型配置分物理集合；不是向量模拟器。
- 配置 Neo4j：真实 Query API 读写适配；只写实体/支撑/块编号，不写正文。
- 配置 Chat：固定 Wiki 生成流程，先产生草稿，不自动发布。
- 配置 Extractor：真实 JSONL 子进程；提供 `examples/extract_explicit.py` 展示扩展边界。

**本轮只用 HTTP 模拟端点测试这些适配器协议，没有运行真实 Qdrant／Neo4j 或商业模型。请勿把适配器存在当作数据库联调已通过。**

## 7. MCP 与外部 Agent

```bash
./bin/xrag mcp --profile team --toolsets core,graph,files,utility
```

stdio 只输出 JSON-RPC。Server 的 `/mcp` 同时提供 Bearer Key 的 JSON POST tools 子集。输入、输出 Schema 与 HTTP 共用。

协议固定 **MCP 2025-06-18 tools 子集**，没有官方 SDK、OAuth、流式 SSE、resources/prompts/Tasks；未在真实商业 Agent 客户端现场验证。不支持的能力明确说明。外部使用指南：[SKILL.md](../../../skills/xrag-retrieval/SKILL.md)。

## 8. 工程与复测

```text
core/              查询、分块、引用、范围、图遍历；不导入数据库
adapters/          模型 HTTP / Qdrant / Neo4j / local/direct / 抽取插件
internal/source/   固定提交Git与完整目录扫描
internal/automation/ 显式来源检查与触发
evaluation/        人工预期与实际命中评分
internal/store/    SQLite 元数据、文件版、任务、条目与范围
internal/build/    Server 构建、发布、固定 Wiki 生成
internal/server/   HTTP、管理动作、Web
internal/cli/      Profile 与 Client 路由，不导入 Server/SQLite
protocols/mcp/     明确范围的 MCP tools 实现
contracts/         单一动作清单和 Schema
cmd/               xrag-server、xrag
```

```bash
make build
make test
make race
make vet
python3 scripts/smoke.py
python3 scripts/smoke_demo2.py
# 可选：安装 Playwright 并有 Chromium 后，执行页面 DOM+真实HTTP桥接测试
python3 scripts/smoke.py --browser
python3 scripts/smoke_demo2.py --browser
```

真实检查范围：checks/REPORT.md（原路径 `checks/REPORT.md`，未归档）。完整差距：[docs/STATUS.md](STATUS.md)。本轮没有宣称生产容量、完整 V0.7 兼容或独立子代理评审通过。

## 9. 数据与后续交接

将本目录放到新的实验目录即可，不要直接覆盖现有仓库。构建产物在 `bin/`，运行数据在 `.xrag-demo/`，原设计不修改。记录、正文、引用和草稿存在 SQLite；源目录另外保存。

`xrag-server backup --data .xrag-demo --out /新路径/backup.sqlite` 使用 SQLite 自身备份能力，包含已入库的正文、版本、配置记录和 Key 摘要。**不含原来源目录、admin.key、providers.json 或外部库。**完整迁移应停机复制整个数据目录和必要的外部配置；不要只复制打开的 `.sqlite` 文件而漏掉 WAL。

许可证未在本 Demo 中擅自决定，见 [LICENSE_NOTICE.md](../../../LICENSE_NOTICE.md)。
