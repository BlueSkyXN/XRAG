# Demo.9 操作说明：MCP 接入、实时状态与明确续跑

`0.7.0-demo.9`；全新数据目录，存储标记 `demo9`。不转换旧软件目录。文件内容更新、原引用、同版备份恢复仍是当前版本的正常能力。

本轮新增的是可以运行的协议与任务功能，不是把未完成项改成“已设计”。外部身份服务、商业模型、真实 Qdrant/Neo4j 及第三方 Agent 实机仍未验证。测试中的身份服务和规划器会明确标为受控端点。

## 1. 启动

```bash
bash demo.sh
# http://127.0.0.1:8765；输入本机终端显示的 Key
./bin/xrag profile add team --server http://127.0.0.1:8765 \
  --key-file "$PWD/.xrag-demo/admin.key" --kb demo
```

Server 构建知识，Client 读取或提交任务；两个程序及 Worker、Web、MCP 独立开关不变。纯远程 Client 不要求 SQLite/C 环境；完整 Server 本机构建需要 Go 1.23+、C 编译器、FTS5 SQLite 开发库和 make。默认不下载模型、不启动外部数据库。

## 2. MCP：JSON、SSE、stdio

### 明确的实现范围

| 项目 | 行为 |
|---|---|
| 协议版本 | 协商 2025-11-25、2025-06-18、2025-03-26；不是 2026-07-28 新线协议 |
| HTTP | POST `/mcp`；Accept JSON 返回 JSON，接受 SSE 时返回请求内的 SSE 流 |
| SSE 内容 | 心跳以及本请求最终 JSON-RPC 响应；不是模型逐 Token 流 |
| HTTP GET / DELETE | 405；没有独立服务端推送流、协议会话或 replay event ID |
| 通知 | 202 空响应；`notifications/cancelled` 可以取消匹配的在途调用 |
| stdio | 每行一个 JSON-RPC 对象；有限并发，让取消通知不被长工具堵住 |
| tools | 16 项查询/公共动作、2 项显式 write、5 项 opt-in research 任务动作 |
| resources / prompts | 检索指南、可见资料目录、原引用阅读、任务使用提示 |
| 未提供 | MCP Tasks、sampling、subscriptions、任意服务器发起请求、完整 MRTR 新协议 |

MCP 请求只接受 Header Bearer 身份，不使用工作台 Cookie。进度流另有工作台接口，见 §4。协议只是入口，所有读取/提交继续调用同一应用操作。

JSON-RPC 一次一个对象，不接收批数组；请求最多 2 MiB。HTTP 最多 128 个在途请求、stdio 最多 8 个并发工具/资源调用；每个调用最多十分钟。相同身份的并发 request ID 必须唯一；冲突明确拒绝，不错误取消另一请求。取消不跨身份。

断开 MCP POST/SSE 不自动取消已经接受的工作。本次请求仍受执行上限与 Server 生命周期约束；明确取消 RPC 可以停止此调用。已提交的持久 Research/Recipe 任务有独立生命周期，取消任务使用 `research_cancel`。

### 使用

```bash
./bin/xrag mcp --profile team --toolsets core,graph,files,utility,research
```

远程 MCP URL 示例：

```text
http://127.0.0.1:8765/mcp?toolsets=core,graph,files,utility,research
Authorization: Bearer <your-key>
Accept: application/json, text/event-stream
MCP-Protocol-Version: 2025-11-25
```

`resources/list` 提供：

- `xrag://guide/retrieval`：静态使用指南，不调用模型。
- `xrag://catalog`：调用当前身份可见的 Discover。

`resources/templates/list` 提供 `xrag://refs/{ref}`；使用已经返回的完整引用，继续走 `files.read`。不能借资源 URI 读取任意 HTTP URL。资源阅读仍可能是有界片段；后续展开用正常读取动作。

`prompts/get` 的 `xrag_retrieve` 接收 `question` 和可选 `kb`。提示解释候选/阅读/统计/图/调查的不同用途，不执行任务，不增加调用范围。

附带 `scripts/mcp_probe.py` 可用 API Key 或已有 access token 检查实际 HTTP 协议：

```bash
python3 scripts/mcp_probe.py --url http://127.0.0.1:8765/mcp \
  --key-file .xrag-demo/admin.key --report /tmp/mcp-probe.json
```

这是本项目自写的协议检查客户端，不冒称第三方 Agent 联调。

## 3. 可选 OAuth 资源服务

本实现让 XRAG **接受外部授权服务器已签发的 access token**。不在 XRAG 内实现用户登录、授权页面、授权码交换或刷新令牌，也不伪造一个全功能 OAuth 服务。API Key 路径继续可以独立使用。

### 配置

复制并填写 `examples/mcp-auth.json`，在 Server 启动：

```bash
./bin/xrag-server serve --data .xrag-demo \
  --mcp-auth /absolute/path/mcp-auth.json
```

配置不从 Web 任意修改。保存文件/读取配置不会调用身份服务，收到带 OAuth token 的 MCP 请求时才向登记的 introspection endpoint 发请求。每次重新检查，不用过期的本地 token 缓存。

| 字段 | 约定 |
|---|---|
| resource | 客户端看到的完整 MCP URL；用于 audience 匹配 |
| issuer | 外部授权服务器的明确标识 |
| introspection_endpoint | 登记的 RFC 7662 端点，POST form + Client Basic 身份 |
| client_secret_ref | Server 能读的 `env:` / `file:` 引用 |
| subjects | 外部 `sub` 到 XRAG reader/contributor/maintainer 与 KB 范围的明确映射 |

请求使用 HTTPS。只有显式 `allow_loopback_http=true` 时允许数值环回地址，供本机测试；不支持任意明文远端或自动退回 HTTP。拒绝跳转，不把 token 发到另一个地址。

身份服务响应必须有 active、sub、iss、aud、exp 和 scope；本实现采用比“仅 active”更明确的部署合同。缺字段、不匹配、过期、未来 nbf、未登记 sub 均拒绝。暂时不可达报 `AUTH_PROVIDER_UNAVAILABLE`，不说用户一定令牌无效。

三个 scope：

- `xrag:read`：MCP 基础访问，必须具备。
- `xrag:write`：仍须本地 Contributor 或以上，只允许明确的草稿/反馈工具。
- `xrag:research`：Research/Recipe 提交、状态、取消和续跑工具组。

scope 与本地角色/KB 范围取交集；scope 不会把 reader 升成 contributor 或 admin。令牌不传给 Qdrant、Neo4j 或模型。OAuth 只在 `/mcp` 上提供；普通 `/v1/*` 和现有 CLI 远程动作仍使用 XRAG API Key，CLI 没有内置 OAuth 登录/刷新命令。

### 元数据

启用后公开 `/.well-known/oauth-protected-resource/mcp` 与根路径 metadata，包含 resource、authorization_servers 和 scopes_supported。401/403 的 WWW-Authenticate 提供 resource_metadata 与 scope 提示。

外部授权服务器仍需提供客户端能发现/使用的 OAuth 流程与相应 token。这里没有代替它签发令牌。本轮验证使用本机受控 RFC 7662 接收端，不代表某个商业 IdP 已接通。

## 4. 实时任务状态

```bash
./bin/xrag jobs watch JOB_ID --profile team --timeout 10m
# 同义入口
./bin/xrag research watch JOB_ID --profile team --timeout 10m
```

输出是 JSON Lines。HTTP 为 `GET /v1/tasks/events?id=...`、Accept `text/event-stream`。事件含：

- task：当前 job 快照，状态、已保存观察与检查点摘要。
- done：任务已经结束。
- error：身份/范围/读取失效。
- reconnect：单次订阅十分钟到期，明确重新订阅即可。

每半秒观察保存状态；不是逐 Token 生成，也不是历史事件日志。重连立即得到当前状态，不声称支持 Last-Event-ID 完整重放。

网页在“委托调查”选择任务后可开启/停止实时查看。离开页面、隐藏页面或退出会结束订阅；不会取消后台任务。API Key 停用会停止后续订阅返回。订阅等待不持有内容共享租约，不长时间阻挡在线清理。

## 5. Research / Recipe 检查点与明确续跑

### 写入哪些状态

```text
ready → planning → planned → executing → ready ... → complete
         调模型前    已存决策   工具调用前    已保存观察与依据
```

保存任务原始输入、固定范围、原开始时刻、观察、依据、已返回引用、重复动作状态和下一步；不保存可执行代码。每次计费调用继续使用原任务 run_id 的既有预算账务。

检查点与结果保留关系同事务更新。相关范围/结果至少保留到原执行期限再加五分钟；成功结果继续按现有规则保留七天。期限延长不复活被明确撤回的来源。

### 如何继续

```bash
./bin/xrag research resume JOB_ID --profile team --wait
./bin/xrag recipe resume JOB_ID --profile team --wait
# API: POST /v1/tasks/resume {"id":"..."}
# MCP research 组：research_resume
```

只继续同一个逻辑任务；重新领取增加 attempt，**不**改变任务 ID、首次开始时间、绝对截止时间、资料范围或已用预算。相同 resume 重复提交不生成第二个任务。

| 已保存阶段 | 处理 |
|---|---|
| ready | 使用已有结果继续下一步 |
| planned | 复用已有模型决策，不重复规划调用 |
| executing 的 files.read/grep/keyword/discover | 在仍可读的固定范围重跑明确的本地只读动作 |
| planning 的 Recipe | 固定配方纯本地决策可以重新计算 |
| planning 的 Research | 规划响应未保存，返回 `RESUME_OUTCOME_UNKNOWN` |
| executing 的语义/+ask/图等 | 可能调用模型或实时后端，不等价重放，返回明确拒绝 |
| complete | 复用已保存最终结果，完成引用保留/任务收尾，不重新调查 |

配置指纹改变、原范围失效、资料域撤回 epoch 改变、截止时间过期、用户取消、原 lease 尚有效、缺检查点，都不会偷偷重建一份新任务。失败不会自动循环重试。

`read-only` 不等于结果确定/没有费用。本实现刻意保守，不重放结果未知的收费调用。未知调用仍占用原预留；已经得到结果但用量不明的旧调用也不清零。需要更换模型、增加时间或扩大范围时，应明确提交新任务，而不是修改旧任务伪装为续跑。

此功能不是透明的任意进程恢复，也不支持任意业务写操作的补偿。仅支持单 KB 有界调查和有限配方；上限继续是 Research 16 步/600 秒、Recipe 15 个只读步骤。

## 6. 平台交付

附带四个纯 Go Client 包：macOS arm64/amd64、Windows amd64、Linux arm64。交叉编译通过不等于目标实机通过；未签名/公证。它们不含 Server，不能据此宣称 Windows 原生 SQLite Server 已移植。

完整 Linux amd64 包在本环境重新解包运行；其余平台需实际机器验证。`scripts/package_clients.py` 提供可重跑的交叉构建。

## 7. 本轮仍未完成

真实 Qdrant/Neo4j/商业模型/S3/WeKnora/第三方 Agent/商业 IdP 联合运行；复杂 direct 与大型图的更充分下推；孤儿产物自动发现；供应商全实例快照；全平台 Server；MCP 2026 新协议、sampling/Tasks/subscriptions/MRTR 等未实现项。不把本轮新增的 SSE 或 OAuth 资源适配说成协议全部完成。

## 技术来源（外部规范，不是产品通过证明）

- MCP 2025-11-25 transports: https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
- MCP 2025-11-25 authorization: https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
- Token introspection RFC 7662: https://www.rfc-editor.org/rfc/rfc7662.html
- Protected resource metadata RFC 9728: https://www.rfc-editor.org/rfc/rfc9728.html

当前实现支持的旧稳定协议修订已明确列出，不把草案/2026 新规范混入同一次兼容承诺。
