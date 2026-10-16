# XRAG 0.10.2

这是 0.10.2 实验版。功能以 Demo.9 为实现基线：目录和 Git 来源、文件版发布、关键词/语义/图检索、三种 Client、MCP、知识草稿与 Wiki、反馈评测，以及有界 Research/Recipe。

它不是生产发行。磁盘格式仍使用 Demo.9 的 `store_schema=demo9`，响应里的 `schema_version` 仍是 `0.7-demo`。不迁移更早的数据目录。真实 Qdrant、Neo4j、商业模型和 S3/WeKnora 现场联调仍未完成。

线上实例：<https://blueskyxn-xrag.hf.space>（HF Space 部署方式见 [deploy/hf/](deploy/hf/README.md)）。

## 1. 从无模型版本开始

```bash
bash demo.sh
```

浏览器打开 `http://127.0.0.1:8765`，输入终端显示的 API Key。脚本创建 `.xrag/`、导入合成示例并启动服务，不下载模型或数据库。需要 Go 1.23+、C 编译器、带 FTS5 的 SQLite 开发库与 make。停止服务按 Ctrl+C，当前数据保留。

工作台在 `/`，取代原来的经典界面，按“查找 / 建设 / 沉淀 / 评估 / 系统”组织全部页面，导航按 Reader/Contributor/Maintainer/Admin 角色显示。检索页把一次查询拆成执行过程、结果路线（关键词/语义/图）、四种完整性，并给出等价的 CLI、HTTP 与 MCP 调用；原文按引用固定的文件版本读取，标出引用行。`⌘K` 列出动作注册表里的全部动作。

前端源码在 `webapp/`（React + Tailwind + Radix），`make web` 构建到 `internal/server/web/dist/`（不进 Git）；开发时 `make web-dev` 启动 Vite（端口 5173，`/v1` 代理到 8765）。发布包和 Docker 镜像在构建时内嵌产物；裸 `go build` 得到的二进制没有界面（`/` 返回 503 提示，API 与 MCP 正常）。只用系统字体，不打包字体文件。

从 [Releases](https://github.com/BlueSkyXN/XRAG/releases) 下载 Linux amd64 完整包，解压后运行 `bash run-linux.sh`。需要 GLIBC 2.34+ 和带 FTS5 的 libsqlite3，Git 来源还需要 git。纯 Client 包不含 Server，不需要 C/SQLite；提供 macOS、Windows amd64 和 Linux arm64 版本。下载后可用随附的 `SHA256SUMS` 校验附件。

## 2. 用 Docker 运行

```bash
docker run -d --name xrag -p 8765:8765 -v xrag-data:/data ghcr.io/blueskyxn/xrag:<tag>
```

镜像按提交打标签（`sha-<短SHA>`）、`main` 跟随主干、版本 tag 跟随发布；任何情况下都不打 `latest`。数据落在卷的 `/data/xrag`，端口默认 `8765`，可用 `XRAG_LISTEN` 覆盖。

首次启动会导入示例，并排队构建 `demo` 库。设置 `XRAG_BUILD_ON_START=<kb>` 后，每次启动都会排队构建该库；KB ID 为 1–64 位字母、数字、下划线或连字符，首位为字母或数字。数据目录中有 `RESTORE_INFO.json` 的恢复副本跳过自动构建。

bind mount 宿主目录时（`-v /host/path:/data`），该目录需要对容器内用户（UID 1000）可写。

两种登录方式，由环境变量决定：

- **API Key 模式**（默认，不设变量）：首次启动生成管理员 Key，浏览器打开 `http://127.0.0.1:8765` 后执行 `docker exec xrag cat /data/xrag/admin.key` 取出并输入。
- **用户名密码模式**：设置 `ADMIN_USERNAME` 和 `ADMIN_PASSWORD`（两者必须同时设置，密码至少 16 位），浏览器直接用账号登录。

`/healthz` 返回版本与所构建的提交 SHA。HF Space 上的部署见 [deploy/hf/](deploy/hf/README.md)。

## 3. 已有功能

| 领域 | 主要能力 |
|---|---|
| 建知识 | MD/TXT、目录、Git、S3/WeKnora适配；字段/词典/解析器；增量/完整/向量/图专项构建 |
| 查资料 | 精确与关键词、语义、原文grep/阅读、范围与分页、关系/路径、固定组合/外部peer |
| 沉淀 | 草稿、修订、发布；文档/图/聚合依据；Wiki生成再生成/链接；目录/Git/S3写回 |
| 调查 | 有界Research、有限Recipe、任务状态、取消、固定预算/范围/期限、检查点及明确续跑 |
| 运行 | Key、配置、反馈/题集/诊断、自动SQLite备份、记录轮转、在线内容批次清理、离线整集合回收 |
| 接入 | API/CLI，MCP JSON/SSE/stdio、tools/resources/prompts、可选外部OAuth资源服务 |

S3/WeKnora/Qdrant/Neo4j/模型的适配代码和受控测试不等于真实部署已通过。当前文本在SQLite/Qdrant，Neo4j存关系和块标识，不另存原文。

## 4. Client

```bash
./bin/xrag profile add team --server http://127.0.0.1:8765 \
  --key-file "$PWD/.xrag/admin.key" --kb demo
./bin/xrag search keyword E203 --profile team
./bin/xrag +ask 日志 --profile team --graph --trace
./bin/xrag files tree --local ./examples/docs --kb demo
./bin/xrag recipe run --profile team --data @examples/recipe-run.json --wait
# Research 需要配置 Chat：
./bin/xrag research run --profile team --data @examples/research-task.json
./bin/xrag jobs watch JOB_ID --profile team --timeout 10m
./bin/xrag research resume JOB_ID --profile team --wait
```

`+ask` 仍为固定检索组合；Research 根据结果选择下一步；Recipe执行明确的只读步骤。它们不自动发布知识或执行业务动作。续跑不会增加旧任务时间/预算，也不会自动重放未知收费调用。

## 5. 模块分别运行

```bash
./bin/xrag-server serve --data .xrag --workers off --web=false --mcp=false
./bin/xrag-server worker --data .xrag
```

没有Worker时任务排队，直接查询已有资料仍可用。Web/MCP/Worker开关独立。无需全部组件一起启动。

## 6. MCP 接入

```bash
./bin/xrag mcp --profile team --toolsets core,graph,files,utility,research
```

HTTP端点 `/mcp` 根据Accept返回JSON或SSE。资源包括检索指南、当前身份可见的目录和固定引用；提示用于教Agent怎样组合操作。

明确支持所述 2025-11-25/2025-06-18/2025-03-26 能力，未实现 2026-07-28 新线全部行为。GET独立推送流/协议会话/Tasks等没有冒充支持。默认API Key；可选 `--mcp-auth examples/mcp-auth.json` 接已部署的外部授权服务器。

```bash
python3 scripts/mcp_probe.py --url http://127.0.0.1:8765/mcp \
  --key-file .xrag/admin.key --report /tmp/mcp-probe.json
```

## 7. 本轮文档

- [Demo.9 操作说明](docs/DEMO9_OPERATIONS.md)：协议、OAuth角色、任务状态和续跑规则。
- [66项功能表](docs/FEATURE_MATRIX.md)、[当前范围](docs/STATUS.md)、[阶段完成情况](docs/COMPLETION.md)。
- [专业连接说明](docs/CONNECTIONS.md)、[真实联调说明](docs/LIVE_INTEGRATION.md)。
- [数据恢复](docs/DATA_RECOVERY.md)、[历史整理](docs/HISTORY_MAINTENANCE.md)、[Demo.8投影恢复](docs/DEMO8_OPERATIONS.md)。
- [实际测试报告](checks/REPORT.md)。历史批次文档仅说明当时增量，以当前STATUS和新操作说明为准。

## 8. 还没有完成的内容

真实 Qdrant+Neo4j+模型+direct 联合运行、真实 S3/WeKnora/商业 IdP/第三方 Agent、真实业务效果；复杂大库下推/实际容量、孤儿产物发现、供应商级联合快照；全平台 Server/Docker/真实代理与浏览器部署。不能用几百项本机/受控测试替代这些工作。

## 9. 开发检查

```bash
make build && make test && make race && make vet
# 仅开发测试/选择CommonMark时需要这些依赖：
python3 -m pip install -r plugins/commonmark/requirements.txt
python3 scripts/check_management.py
python3 scripts/smoke_demo9.py --browser
python3 scripts/smoke_workbench.py --browser
python3 scripts/package.py --out /tmp/xrag-release
python3 scripts/package_clients.py --out /tmp/xrag-release
```

`package.py` 要求工作区干净（包括未忽略的未跟踪文件），从 Git 跟踪清单生成源码包；被过滤规则排除的已跟踪文件会列出并报错。完整 Linux 包还会检查 `bin/xrag` 与 `bin/xrag-server` 的 Linux amd64 平台、Go 程序路径和编译时注入的当前提交。在 macOS 等其他平台可加 `--source-only` 仅生成源码 ZIP。源码、Linux 和 Client 包名均包含当前版本号；Linux CI 会重新解包源码和完整包，执行构建、测试、服务启动与工作台检查。

`--browser` 另需Playwright/Chromium（`XRAG_CHROMIUM` 可指定浏览器路径），Schema检查需jsonschema。默认产品启动不依赖测试工具。旧smoke脚本编号是添加批次，执行的是当前程序与新的临时库，不是兼容测试；其中 demo.8 及更早批次的 `--browser` 针对已移除的经典界面，浏览器检查只在 `smoke_demo9.py` 与 `smoke_workbench.py` 中维护。`smoke_workbench.py` 不加参数时只检查工作台依赖的 HTTP 契约，CI 中运行（先 `make web`）。`go test` 用注入的假前端产物，不依赖 Node。

## 10. 许可与数据

本项目按 [GPL-3.0](LICENSE) 发布，说明见 [LICENSE_NOTICE.md](LICENSE_NOTICE.md)。包里没有模型权重、字体、运行数据或真实凭据。
