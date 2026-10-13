> 当前版本为 Demo.5。本文件保留对应模块的原操作说明；新增行为或冲突以 [DEMO5_OPERATIONS.md](DEMO5_OPERATIONS.md) 与 [STATUS.md](STATUS.md) 为准。不要求旧软件迁移。

# Demo.4 操作：Wiki 主题、发布写回与多来源

本文件对应 `0.7.0-demo.4` 实际代码。CLI 示例均在已保存的 `team` Server profile 上运行。

## 1. Wiki 生成与再生成

生成：工作台“知识沉淀→Wiki”，或：

```bash
./bin/xrag wiki generate --profile team --wait \
  --data '{"topic":"日志","path":"manuals/","single_pass":false}'
```

`path` 是可选库内目录前缀，不是服务器绝对目录。默认先大纲后正文；single_pass=true 才一次调用。两次都计入原有调用尝试记录。没有 Chat、没有该范围内的有效原始资料，就明确失败，不生成空页面。

每次固定现有文件视图；以关键词候选选取最多40块、40000字符的原始来源正文。当前保守排除全部 managed 知识条目和 Wiki，防止自己引用自己。它不是完整多轮调查、不穷尽全部资料、也没有让向量/图参与这条 Wiki 证据选择流程。

```bash
./bin/xrag knowledge revisions ITEM_ID --profile team
./bin/xrag wiki regenerate ITEM_ID --profile team --wait \
  --data '{"expected_revision":"当前草稿修订","path":"manuals/"}'
```

再生成只接受 Wiki 条目；expected_revision 必须是当前 draft，而不是随便填旧 published。任务启动时再检查一次，避免队列等待后覆盖已经修改的草稿。输出保存同一个 item 的新修订，原 published_revision 不变；生成失败不覆盖草稿和发布版。

生成结果必须带至少一个合法 `[n]` 且编号在选定资料范围内；这只能检查引用格式，不证明每句话语义正确。生成期间出现明确撤回时不保存为有效草稿。

发布仍为现有独立动作：

```bash
./bin/xrag knowledge publish ITEM_ID --profile team --wait \
  --revision '要发布的草稿修订'
```

移出当前发布视图：

```bash
./bin/xrag knowledge archive ITEM_ID --profile team --confirm \
  --data '{"revision":"当前已发布修订"}'
```

archive 保留允许读取的历史，不是强制撤回。用户明确撤回资料的原有操作仍阻止相应旧引用。revision历史最多200项，超限报错，不把前200项称为全部。

## 2. 显式写回目录

先在 Server 机器创建一个专用现存目录，编辑 `examples/sync-target-path.json` 的 root：

```bash
./bin/xrag sync-target add --profile team --confirm --data @examples/sync-target-path.json
./bin/xrag sync-target list --profile team
```

登记不生成文件、不安装后台同步。目标ID、路径和分支是固定绑定；要换地址登记新ID，当前没有目标原地修改/删除界面。

将 `examples/knowledge-sync.json` 里的 revision 改为当前已发布版本，再执行：

```bash
./bin/xrag knowledge sync ITEM_ID --profile team --confirm --wait \
  --data @examples/knowledge-sync.json
./bin/xrag knowledge sync-status ITEM_ID --profile team
```

写入路径为 `目标prefix/知识库/item_id.md`，包含发布正文、知识身份、修订和原引用。只允许同KB目标、当前发布版。草稿、blocked、archived不可写回。一次写回最大2MiB，目标位置属于Server机器。

相同内容重复写回不改变文件；目标已有他人内容或与上次成功回执摘要不同则 SYNC_CONFLICT。写入前检查目标和来源；文件实际写出后遇到进程退出，下一次通过相同内容识别幂等完成。

XRAG 使用目录锁、临时文件、落盘和替换协调自己的写者。**不遵守这个锁的外部编辑器仍可能在最后检查与替换之间竞争，本版不是操作系统级通用CAS文件系统。**使用专用输出目录，不要将它作为多人同时编辑文件的事务系统。已送到外部的文件也不会因后来撤回而自动从外部抹除。

回执保存每目标每条目的最近成功版本；不是完整远程版本管理。

## 3. Git 专用分支

编辑 `examples/sync-target-git.json`：root 为 Server 上已存在且有初始提交的本机仓库，branch 必须位于 `xrag/` 下。

```bash
./bin/xrag sync-target add --profile team --confirm --data @examples/sync-target-git.json
./bin/xrag knowledge sync ITEM_ID --profile team --confirm --wait \
  --data '{"target_id":"git-out","revision":"当前发布修订"}'
```

使用独立临时 index 和 Git plumbing 命令，更新 `xrag/knowledge` 等指定分支，不 checkout、不改变用户暂存区、不改主分支、不执行仓库hook、不自动push。通过旧分支SHA更新，冲突时失败，不强制覆盖。

Git目标现有文件若是可执行文件、符号链接或被人改写，就报冲突。远程地址、SSH推送、凭据助手不在此实现中。推送由使用者之后明确执行。

## 4. 防止写回副本重复进入知识库

构建器仅在以下三项同时匹配时跳过同步副本：登记的目标/原来源位置、回执中的相对路径、规范内容摘要。仅伪造一个 `xrag_id` 不会使普通资料被跳过。

目标中某文件经外部修改后，摘要不再匹配，会成为普通来源内容；不是无声丢弃。是否重新纳入来源、移到原分支等由维护者决定。

## 5. S3 兼容来源

只读、path-style endpoint。编辑 `examples/source-s3.json`，设置Server可访问的origin（不要带URL子路径）、桶、前缀、区域和env/file凭据引用：

```bash
export S3_ACCESS_KEY='由使用者设置'
export S3_SECRET_KEY='由使用者设置'
./bin/xrag source add --profile team --data @examples/source-s3.json
./bin/xrag build run demo --profile team --wait
```

环境变量必须在Server和Worker启动环境中存在，仅在Client设置不会传给服务器。也可以在JSON里使用 `file:/绝对路径`。实例支持可选 session_token；anonymous=true 时不接受任何凭据字段。

实现 ListObjectsV2 完整分页、SigV4签名、If-Match读取、ETag/长度判断和前后目录比对。来源上限5000支持文件、单文件5MiB、总64MiB；任何支持文件获取失败或分页异常，保留该来源原发布内容，不按缺席删除。只处理MD/Markdown/TXT。

双次枚举不等于原子桶快照，consistency=object_conditional_live。当前不自动获取STS/IAM角色凭据、不支持Virtual-host-only部署、不做对象存储写回和外部版本迁移。仅对本文合同实现的服务可尝试接入，**本轮未实际运行S3或MinIO**。

## 6. WeKnora 解析文本来源

编辑 `examples/source-weknora.json`：root为origin、knowledge_base为上游库ID、api_key引用实际Key。默认X-API-Key；可显式选Authorization（自动Bearer）。

```bash
./bin/xrag source add --profile team --data @examples/source-weknora.json
./bin/xrag build run demo --profile team --wait
```

按上游API读取知识列表，再按文档读取启用的 text chunks，按 chunk_index+id 排序。导出为 `<knowledge_id>.md`，说明这是解析文本导出、保留分块边界与重叠，不声称还原原PDF/图片/表格。

接口依赖：

- `GET /api/v1/knowledge-bases/{id}/knowledge?page=...&page_size=100&sort_by=created_at&sort_order=asc`
- `GET /api/v1/chunks/{knowledge_id}?page=...&page_size=100&chunk_type=text`
- 响应success、data、total、page、page_size及文档/分块字段需符合目前适配。

某文档尚未completed、枚举失配、分块下载失败就保留旧来源；disabled文档明确跳过。前后文档目录比对只能检测可观察变化；分块编辑未反映到文档更新时间时，不能保证整个上游快照。consistency=upstream_live_export。

不是接管上游建库，也不是实时 WeKnora 搜索代理。**本轮读过官方接口源码并做受控响应测试，没有连接用户实例；不同部署版本仍需联调。**

## 7. 与既有自动更新的关系

新来源仍沿用完整扫描和原任务队列，可手动/API或按来源every_seconds触发。watch_seconds只给path/builtin；新来源没有增加平台原生Webhook适配。当前定时读取可能下载资料计算指纹，设置过短会增加网络/I/O。

所有新写操作都走Server。Client local/direct仍只读，不在本机私自构建或写回。
