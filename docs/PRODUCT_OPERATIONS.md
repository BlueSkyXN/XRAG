> 当前版本为 Demo.5。本文件保留对应模块的原操作说明；新增行为或冲突以 [DEMO5_OPERATIONS.md](DEMO5_OPERATIONS.md) 与 [STATUS.md](STATUS.md) 为准。不要求旧软件迁移。

# Demo.3：配置、反馈、接入与日常管理

**描述实际0.7.0-demo.3接口，不是将来的产品设计。**Server读取知识数据；Web和CLI通过相同管理API工作。没有新增中央服务或常驻管理进程。

> Demo.4继续保留本文既有命令；本轮新增能力与限制见[LIFECYCLE_AND_SOURCES](LIFECYCLE_AND_SOURCES.md)和[STATUS](STATUS.md)。

## 1. 从概览开始

概览展示当前身份可见的知识库、已发布文件数、来源数、知识条目数、来源错误以及任务数量。数据库文件大小和SQLite版本只供Admin查看。这是数据库当前计数，不是磁盘/CPU性能监测或后台探测。

五步引导：准备资料 → 构建 → 首次查询 → 按需配置模型 → 处理反馈。每步链接现有功能，不强迫先配置模型或图数据库。

## 2. 保存模型与后端配置

Web：设置与帮助 → 模型与后端。`GET /v1/settings/providers`返回：

- `revision`、`updated`：已保存的配置修订；
- `config`：下次启动使用的数据库配置；
- `active`：当前API进程正在使用的配置；
- `source=managed|external_file`；
- `editable`、`restart_required`；
- `model_calls=0`说明查看不调用模型。

```bash
./bin/xrag settings show --profile team
# 编辑expected_revision为刚才读取的revision，再保存
./bin/xrag settings save --profile team --data @examples/settings.chat.json
```

提交格式：`{expected_revision, config}`，其中config对象接受embedding/chat/rerank/qdrant/neo4j。空对象清除保存配置。修订不符返回409 VERSION_CONFLICT，需重新读取；不自动覆盖他人改动。

只保存env/file引用，不保存明文凭据。引用由实际执行的机器解析，所有Worker也必须有相应环境或文件。保存时不解析引用、不发网络探测；因此保存成功不意味着模型现在可用。

**生效顺序**：保存 → 停止Server与所有Worker → 不带--config重启 → 查看active → 按需要显式重建。

显式 `--config` 或启动脚本 `XRAG_DEMO_CONFIG` 优先，Web只读。该外部文件仍可配置JSONL抽取命令；管理API不接受外部执行器命令。不会因为点保存就执行任意插件。

更换向量模型语义身份、前缀、维度会使旧向量不匹配；重建是另一个操作。已有内容不因配置保存消失。当前未实现配置热更新、跨进程滚动切换或自动模型迁移。

## 3. 访问Key

Web：设置与帮助 → 访问Key，仅Admin管理。

| 动作 | API / CLI |
|---|---|
| 列表 | GET /v1/keys；`xrag keys list` |
| 创建 | POST /v1/keys；`xrag keys create --data @examples/key-reader.json` |
| 改名 | PATCH /v1/keys/{id}；`xrag keys rename ID --name 新名称` |
| 停用 | DELETE /v1/keys/{id} body confirm=true；`xrag keys remove ID --confirm` |

所有CLI加 `--profile team`。创建字段name、role、kbs、可选expires_at（Unix秒；0不自动过期）。过期时间须在创建时的未来；过期或停用后，已存在的Web会话在后续请求也失效。

四角色继承，不引入用户目录：Reader查询；Contributor增加草稿/反馈；Maintainer增加来源、构建、发布与评测；Admin全工作空间。普通角色须有非空且存在的KB集合。Admin的scope为全局，不将传入KB列表伪装为受限Admin。

Token只在创建响应显示一次，列表只返回元信息。名称可改，角色/范围/期限修改暂不实现；需要变更先建新Key、确认可用，再停旧Key。最后一个永久Admin不能停用，临时Admin不能取代它的恢复作用。

**退出登录**只删除当前Web会话，不等于停用Key；应用仍可用原Key调用。换身份会清空页面上一次的结果与Key显示。

## 4. 反馈处理

结果卡片“反馈”打开实际提交表单：有用、不相关、内容有误、已过时，可补充说明。原图和查询逻辑不自动修改。

列表 `GET /v1/feedback?kb=demo&status=open&limit=20&offset=0`。

- limit 1–100；offset 0–100000；返回total和next_offset；
- Contributor只看到自己的记录，Maintainer在KB范围内处理；
- `status`可空或open/resolved/dismissed；
- 来源已撤回时，ref/path/note/resolution不返回，以免通过旧反馈继续读取撤回内容。

```bash
./bin/xrag feedback list --profile team --limit 20 --offset 0
# ID、expected_revision取实际列表值
./bin/xrag feedback resolve FEEDBACK_ID --profile team \
  --data @examples/feedback-resolution.json
```

PATCH字段status、resolution、expected_revision。处理采用修订匹配；反馈可以重新打开。界面“导出本页”只导出当前20条及页参数，明确不是所有反馈的一键全量导出。

本轮没有反馈自动训练、自动加权排名、自动转评测题。先把处理结果记录下来，再由维护者决定修改资料或加入题集。

## 5. 已发布资料导出

```bash
./bin/xrag kb export demo --profile team --out published.zip
```

需要Maintainer或Admin。ZIP包括 `documents/<库内路径>` 当前已发布MD/TXT，及 `manifest.json` 来源/edition/hash/ref清单。只读发布视图；不含草稿、撤回内容、Key、历史版本和外部向量/图。处理过程中发生撤回会拒绝该导出；普通新版本出现不混入已选定视图。

这适合交接可读资料，**不是可恢复整套工作空间的备份**。限制10000文件、64MiB正文，超过报错，不截取前一批当完整导出。

## 6. 快照、离线恢复、清理

完整操作与边界见[DATA_RECOVERY](DATA_RECOVERY.md)。日常入口：

```bash
./bin/xrag backup create --profile team --confirm
./bin/xrag backup list --profile team
./bin/xrag backup download BACKUP_ID --profile team --out snapshot.sqlite
./bin/xrag backup remove BACKUP_ID --profile team --confirm
./bin/xrag maintenance preview --profile team
./bin/xrag maintenance clean --profile team --confirm
```

创建快照可能占用I/O和数据库句柄，不承诺任何负载下无影响。最多登记20份；用户下载后明确删除旧快照，不自动轮转。

清理只删除**已经过期**的结果/句柄缓存和查询摘要。不删除正文、历史edition、知识条目、模型缓存或任务报告，也不执行自动VACUUM。它不等于全历史GC，也不承诺立即减少物理文件大小。

## 7. 本地profile与文件输出

```bash
./bin/xrag profile list
./bin/xrag profile rename team company
./bin/xrag profile use company
./bin/xrag profile remove company --confirm
```

改名保留内部profile_id；删除仅删除客户端连接，不动服务器知识。当前默认删除后置空；显式指定不存在的profile报错，不静默回退localhost。列举不解析秘密引用，兼容旧明文字段会被遮蔽。

`--out`用于管理JSON结果及明确的二进制下载命令。目标存在拒绝覆盖，父目录需要存在。数据仅在HTTP成功后写入；无法完成则移除本次不完整文件。下载超时用CLI `--timeout`控制。

## 8. 对外接口清单与已知限制

新路由见 `contracts/management/product-routes.json`，请求Schema见同目录。Go处理器还检查引用、角色、字段组合、当前修订等业务条件；Schema文件不是运行时的全部逻辑。

帮助页面提供当前实际命令和能力边界。没配置Chat时Wiki拒绝，不给假正文；实际模型、Qdrant、Neo4j仍需真实环境验证。团队身份管理目前是API Key维度，不含密码账号、邀请、SSO、多Key归属或组织树。
