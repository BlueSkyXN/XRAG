# 工作台与日常管理

工作台位于 Server 的 `/`，导航按角色和知识库范围显示。编译、首次运行见 [README](../README.md)。

## 页面与角色

| 分组 | 页面 | 用途 |
|---|---|---|
| 查找 | 检索、关系、调查 | 候选资料、原文、图与多步任务 |
| 建设 | 资料与来源、任务 | 来源、构建、发布文件和后台任务 |
| 沉淀 | 知识与 Wiki、反馈 | 草稿、发布、写回和反馈处理 |
| 评估 | 评测 | 题集、策略比较与查询诊断 |
| 系统 | 概览、接入、设置 | 当前规模、Key/CLI/MCP、模型和维护 |

`⌘K` / Ctrl+K 打开命令面板。检索结果可展开执行过程、路线、完整性和等价调用；原文按引用的文件版展示。页面可见性之外，Server 还会检查每个操作的权限。

| 角色 | 主要权限 |
|---|---|
| Reader | 可见 KB 的检索、阅读与调查任务 |
| Contributor | Reader，加草稿与反馈提交 |
| Maintainer | Contributor，加来源、构建、知识发布、评测与资料导出 |
| Admin | 全工作空间管理，包括 Key、服务配置、备份和维护 |

普通角色绑定非空 KB 集合，Admin 为全局身份。API Key 是应用身份；容器密码入口提供管理员登录，不包含多用户密码目录、邀请或组织树。

## 登录与 Key

默认用初始化产生的 `admin.key` 登录。容器同时设置 `ADMIN_USERNAME`、`ADMIN_PASSWORD` 后启用密码模式：用户名为 1–64 个字母、数字或 `_.@-`，密码至少 16 字符、单行。CLI/API 继续使用 API Key，不能将网页登录密码当 Bearer token。

```bash
./bin/xrag keys list --profile team
./bin/xrag keys create --profile team --data @examples/key-reader.json
./bin/xrag keys rename KEY_ID --name 新名称 --profile team
./bin/xrag keys remove KEY_ID --confirm --profile team
```

新 token 只在创建时显示；列表只返回元数据。Key 支持名称、角色、KB、可选 `expires_at`；期限为 Unix 秒，0 表示不过期。现有 Key 不能原地更改角色/范围/期限，需要创建替代 Key 并验证，再停用旧 Key。最后一个永久 Admin 不能停用。

退出工作台会清除当前会话；停用或过期 Key 才会阻止该身份继续请求。下载快照等管理产物可能包含敏感资料，应按资料权限保存。

## 配置与预算

```bash
./bin/xrag settings show --profile team
./bin/xrag kb settings demo --profile team
./bin/xrag usage show --profile team
```

服务级设置返回保存修订 `revision`、保存的 `config`、当前运行的 `active`、配置来源与 `restart_required`。保存时使用 `expected_revision`。服务级配置需重启 Server 和全部 Worker；外部 `--config` 优先。完整步骤见 [连接配置](CONNECTIONS.md)。

KB 设置包含 providers、字段、词典、解析/图配置和 budget。Maintainer 可修改允许的处理配置；变更模型地址和凭据需 Admin。字段支持 string/number/boolean/date，date 使用 YYYY-MM-DD；分块大小为 100–20000 字符。无效字段值会阻止该文件的新版本发布。

预算可限制每逻辑请求/任务的调用数、Token 与金额，并按 UTC 日期汇总。发出前预留，收到 usage 后结算；结果或费用未知保留预留。金额限制需提供对应 rates。真实 usage 超过预留时记录实际值并阻止后续调用，已经发生的远端费用不能撤销。

构建、常规模型检索、Wiki、评测、Research 与主线的 `image.describe` 使用此机制。图片请求的预留包含发送给模型的 base64 数据，详见 [图片描述](IMAGE_DESCRIBE.md)。外部 peer 的内部费用与 direct 模式调用也不由本地 Server 账本覆盖。

## 反馈处理

结果卡片可提交“有用、不相关、内容有误、已过时”及说明。Contributor 查看自己的记录，Maintainer 可在 KB 范围内处理：

```bash
./bin/xrag feedback list --profile team --limit 20 --offset 0
./bin/xrag feedback resolve FEEDBACK_ID --profile team \
  --data @examples/feedback-resolution.json
```

处理采用 `expected_revision`，状态为 open/resolved/dismissed，可重新打开。分页 limit 为 1–100、offset 为 0–100000；“导出本页”仅含当前页。明确撤回的依据会使相关引用和内容停止返回。整理成题集的流程见 [效果比较](OPERATIONS.md)。

## 分开运行组件

```bash
./bin/xrag-server serve --data .xrag --workers off --web=false --mcp=false
./bin/xrag-server worker --data .xrag
```

以上两个进程需分别在终端或服务管理器中运行。API-only 可入队，已有发布资料可查询；Worker 负责构建、知识、评测、调查与维护。Web/MCP 开关独立。每个数据目录只能有一个 API Server。

## 启动参数与容器变量

| 场景 | 参数/变量 | 默认与含义 |
|---|---|---|
| Server | `--data` / `--listen` | `.xrag` / `127.0.0.1:8765` |
| Server | `--workers` | embedded；设 off 后由独立 Worker 执行 |
| Server | `--build KB` | 启动时显式排队构建该库 |
| 演示/发行脚本 | `XRAG_DEMO_DATA`、`XRAG_DEMO_LISTEN`、`XRAG_DEMO_CONFIG` | 数据目录、监听与可选 providers 文件 |
| 通用容器 | `XRAG_DATA_DIR`、`XRAG_LISTEN` | `/data/xrag`、`0.0.0.0:8765` |
| 通用容器 | `XRAG_BUILD_ON_START` | 每次启动排队的 KB；未设置时只在首次初始化构建 demo |
| 通用容器 | `ADMIN_USERNAME`、`ADMIN_PASSWORD` | 同时设置才启用密码登录 |
| HF 包装入口 | `PORT` | 默认 8765，转换成 XRAG_LISTEN；默认每次构建 demo |
| 可信反向代理 | `XRAG_TRUST_FORWARDED_PROTO` | 仅在受控代理入口启用，使用转发协议设置 Secure cookie；HF 包装层已启用 |

恢复目录含 `RESTORE_INFO.json` 时，脚本和容器跳过自动构建。`/healthz` 为公开存活检查，返回程序版本与构建提交；它不检查模型、外部数据库或业务效果。

## 客户端与输出文件

```bash
./bin/xrag profile list
./bin/xrag profile rename team company
./bin/xrag profile use company
./bin/xrag profile remove company --confirm
```

profile 改名保留内部 ID，删除只移除客户端连接。明确指定不存在的 profile 会报错。`--out` 目标须尚不存在、父目录已存在；HTTP 失败不写成功产物。

备份、恢复、自动计划与清理分别见 [DATA_RECOVERY.md](DATA_RECOVERY.md) 和 [HISTORY_MAINTENANCE.md](HISTORY_MAINTENANCE.md)。
