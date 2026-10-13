# XRAG 0.7.0-demo.3

**可运行的 Go 知识检索 Demo。Server 建设知识，Client 查询；本版补齐配置、接入、反馈处理和数据恢复。**

不是完整 V0.7 的完成版。`+ask` 只组合检索，不生成回答；Recipe/Research 仍未实现。默认不连接模型、Qdrant 或 Neo4j，先用真实 SQLite 资料检索和明确标注关系体验。

## 1. 最快启动

解压到新目录，在项目根目录运行：

```bash
bash demo.sh
```

浏览器打开 `http://127.0.0.1:8765`，填写终端显示的 API Key。脚本编译两个程序、初始化独立的 `.xrag-demo/`、导入七份合成资料，然后启动服务。Ctrl+C 停止，数据保留。不是后台安装器，不下载模型、不安装系统软件。

本机构建：Go 1.23+、C 编译器、带 FTS5 的 SQLite 开发库、make；Git 来源需要 git，抽取器示例需要 Python3。Server 使用 CGO，Client 构建为 CGO_ENABLED=0。

macOS 使用源码本机编译，需 Xcode Command Line Tools 等本机构建条件；也附 `启动Demo.command`。**本轮没有 macOS 或 Windows 实机测试。**Server 原生锁使用 Unix flock；Windows 可在匹配的 Linux/WSL 环境尝试，不承诺原生包。

Linux amd64 程序包使用 `bash run-linux.sh`。本轮在 Debian13 系统 SQLite3.46.1/glibc 环境验证，其他发行版动态库不兼容时改用源码编译。

## 2. 先体验完整使用流程

| 入口 | 可以实际完成的事 |
|---|---|
| 使用概览 | 查看已发布文件、来源、知识条目、来源错误、任务状态；按五步引导开始；创建知识库 |
| 检索试用 / 关系探索 | BM25/编号、原文、范围、分页、关系路径；提交有用/不相关/有误/过时反馈 |
| 资料与构建 | 上传MD/TXT，登记目录或Git，设置自动检查，查看逐文件构建详情 |
| 知识沉淀 | 创建和编辑草稿、明确发布；未发布修改不覆盖旧正文；配置Chat后生成Wiki草稿 |
| 检索评测 | 小型人工题集、固定策略比较、遗漏与实际过程，不用TopK作业务统计 |
| 反馈处理 | 按状态分页，读原引用，保存处理说明、重新打开或不采纳；导出当前反馈页 |
| 设置与帮助 | 保存模型/后端设置、管理Key、导出资料、创建/下载/删除数据库快照、清理过期缓存、查阅操作帮助 |

所有界面使用实际HTTP接口，不是画出来的静态样例。演示资料是合成内容。概览不提供虚构的“完成百分比”。

**默认示例**：关键词搜“分页样本记录”得到37条，按10/10/10/7续读；E203是完整编号匹配；F1只提实体，F2才支持关系，within=F1不会引入F2的边。图来源为明确的 `[[设备A]] --使用--> [[组件B]]` 标注，不是自动发现事实。

## 3. 保存连接后使用 CLI

```bash
./bin/xrag profile add team --server http://127.0.0.1:8765 \
  --key-file "$PWD/.xrag-demo/admin.key" --kb demo

./bin/xrag overview --profile team
./bin/xrag search keyword E203 --profile team
./bin/xrag +ask "组件B" --profile team --graph --trace
./bin/xrag files read --ref '实际返回的xr1引用' --profile team
./bin/xrag search keyword 日志 --local ./examples/docs --kb demo
```

纯远程调用不打开本地资料数据库；local只读扫描，不建设持久索引。所有写入由Server执行。未知显式profile报错，不自动请求localhost。`--dry-run`只解释请求。

```bash
./bin/xrag settings show --profile team
./bin/xrag keys list --profile team
./bin/xrag feedback list --profile team --limit 20
./bin/xrag backup create --profile team --confirm
./bin/xrag backup list --profile team
./bin/xrag backup download BACKUP_ID --profile team --out snapshot.sqlite
./bin/xrag kb export demo --profile team --out published.zip
```

`BACKUP_ID`替换成实际返回编号。`--out`只创建新文件，存在则拒绝，不覆盖。JSON结果也可保存到新文件。二进制下载不混入stdout。

## 4. 模型设置保存与生效

「设置与帮助 → 模型与后端」支持Embedding、Chat、Rerank、Qdrant、Neo4j。只配置需要的部分，凭据填写 `env:NAME` 或 `file:/absolute/path` 引用。

保存会写入数据库，**不会立即切换运行模型，也不会探测/付费调用**。需要停止并重启Server和全部同机Worker。不带 `--config` 的进程读取保存设置；显式 `--config providers.json` 优先，此时Web设置只读。更换Embedding模型/前缀/维度需要显式重建，保存本身不重建。

已有模型/专业后端代码仅用受控端点测试，本轮无真实商业模型或Qdrant/Neo4j实例联调。详见[连接说明](docs/CONNECTIONS.md)。

## 5. 访问 Key 与反馈

延续Reader / Contributor / Maintainer / Admin四角色；新页面管理实际应用Key。**一把Key就是一个调用身份，不是独立用户账号下多Key模型**，没有新建密码登录/SSO目录。

可创建指定KB范围的Key、改名、设置创建时有效期、停用。明文只在创建响应显示；列表不返回Key或摘要。不能停用最后一把永久管理员Key。Admin仍是全工作空间管理员，不能称“只管一个KB的Admin”。

反馈提交不自动改排名或进入题集。维护者要写处理结果；Contributor只看自己的反馈。来源明确撤回后，旧反馈不继续返回引用正文及可能包含该内容的说明。

## 6. 导出、备份和恢复是三件事

- **资料导出**：当前已发布正文ZIP＋版本清单；没有草稿、历史、Key和外部索引。不是整库备份。
- **数据库快照**：SQLite中的正文/历史/草稿/任务/Key摘要等；不含来源目录、明文Key文件、外部providers.json、Qdrant/Neo4j。
- **离线恢复**：从SQLite快照生成一个全新目录，保留资料origin和旧ref；禁用旧Key、自动扫描、保存的模型设置和未完成任务，生成recovery-admin。

```bash
# 恢复目标必须不存在，其父目录必须存在
./bin/xrag-server restore --from snapshot.sqlite --data /absolute/new-xrag-data --confirm
# 先只启动查询，不自动重建
./bin/xrag-server serve --data /absolute/new-xrag-data --listen 127.0.0.1:8765 --workers off
```

原数据库不会被覆盖。恢复之后用新目录的admin.key；逐项确认来源和模型再重建。详见[恢复说明](docs/DATA_RECOVERY.md)。

## 7. 分开执行器与MCP

```bash
# 终端A
./bin/xrag-server serve --data .xrag-demo --workers off
# 终端B
./bin/xrag-server worker --data .xrag-demo
# 外部Agent的本机stdio入口
./bin/xrag mcp --profile team --toolsets core,graph,files,utility
```

模型配置变更要同时重启A/B。管理功能不会把Key、备份、设置编辑工具加入默认MCP工具集。MCP仍为固定2025-06-18 tools子集，非官方SDK完整实现；实际商业客户端未联调。外部说明：[SKILL.md](skills/xrag-retrieval/SKILL.md)。

## 8. 升级、开发和范围

Demo.2原目录升级保持Key/引用，不等于上述“恢复到新目录”。操作见[升级说明](docs/UPGRADE.md)。

```bash
make build
make test
make race
make vet
python3 scripts/smoke.py
python3 scripts/smoke_demo2.py
python3 scripts/smoke_demo3.py
# 配备Playwright和Chromium后可加 --browser
python3 scripts/upgrade_smoke.py --old-source /path/to/XRAG_V0.7_Demo2_源码.zip
python3 scripts/check_management.py
```

[产品操作](docs/PRODUCT_OPERATIONS.md)｜[持续更新与评测](docs/OPERATIONS.md)｜[实际范围](docs/STATUS.md)｜[本轮报告](checks/REPORT.md)

当前仍是单机SQLite Demo：没有完整历史GC、生产级容量证明、OSS/WeKnora、Recipe/Research、完整模型账务。过期缓存清理不等于完整数据生命周期管理。单次已发布资料导出上限64MiB/10000文件；最多20份登记快照；恢复快照上限2GiB。

本轮不修改用户仓库或发布许可证。见[LICENSE_NOTICE](LICENSE_NOTICE.md)。
