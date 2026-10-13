> 当前版本为 Demo.5。本文件保留对应模块的原操作说明；新增行为或冲突以 [DEMO5_OPERATIONS.md](DEMO5_OPERATIONS.md) 与 [STATUS.md](STATUS.md) 为准。不要求旧软件迁移。

# Demo.3：数据导出、数据库快照与恢复

本文件对应实际程序，不承诺外部数据库的统一快照或完整灾备。

> Demo.4继续保留本文既有命令；本轮新增能力与限制见[LIFECYCLE_AND_SOURCES](LIFECYCLE_AND_SOURCES.md)和[STATUS](STATUS.md)。

## 1. 先选正确的产物

| 产物 | 包含 | 不包含 |
|---|---|---|
| 已发布资料ZIP | 当前可见MD/TXT正文、来源/版本/ref清单 | 草稿、旧版、任务、Key、外部索引 |
| SQLite快照 | 已入库正文、历史edition、知识草稿/发布、关系记录、元数据、任务、反馈、评测和Key摘要 | 来源目录、admin.key明文、外部providers.json、env/file引用的值、Qdrant/Neo4j |
| 停机完整目录复制 | 本地目录中的文件 | 目录外来源/配置、环境变量、远端后端仍需单独处理 |

备份中可能包含尚未物理清除的历史内容，因此只向管理员开放；应用层撤回不自动擦除已下载的旧快照。

## 2. 创建与下载数据库快照

```bash
./bin/xrag backup create --profile team --confirm
./bin/xrag backup list --profile team
./bin/xrag backup download BACKUP_ID --profile team --out snapshot.sqlite
```

也可在「设置与帮助 → 数据管理」完成。Server在自己的data_dir/backups写新文件，SQLite负责快照读取；完成后登记大小、SHA-256和创建时间。输出失败不会登记为成功备份。最多20份登记快照，不提供自动日备计划或上传云盘。

`xrag-server backup --data .xrag-demo --out NEW_FILE.sqlite`仍保留原本机命令，不进入Web登记列表。

在线快照是SQLite一致状态，不是来源文件、SQLite和外部数据库共同冻结。不要承诺跨后端原子备份。创建/下载会占用I/O，客户端默认请求上限120秒；大库应在维护时段使用本机命令。

## 3. 恢复只写新目录

```bash
./bin/xrag-server restore --from snapshot.sqlite \
  --data /absolute/parent/restored-xrag --confirm
```

要求：父目录已存在，restored-xrag目录**尚不存在**，输入为本Demo SQLite快照，最大2GiB。程序复制到新目录，检查SQLite完整性和必要XRAG表，再准备恢复运行状态。任何一步失败只清理由它新建的目标目录，不删除输入快照，不覆盖源数据。

成功之后：

- **保持**origin、知识库、文件/edition、固定引用、知识条目与已有记录。
- **禁用旧Key**，生成独立 `recovery-admin`，明文放新目录admin.key；旧调用方不能无意连到恢复副本。
- **关闭来源自动扫描和Hook**，避免旧路径立即参与重建。
- **清除保存的模型配置**，避免新机器无意调用旧端点；显式--config仍由操作者自行决定，恢复不会更改外部文件。
- **未完成任务结束为failed**，发出但尚未知的调用标outcome_unknown，不宣称透明续跑。
- **清除临时范围/分页缓存和旧快照列表**；静态旧文件引用仍可读。
- 保存RESTORE_INFO.json说明恢复行为。

启动恢复副本：

```bash
./bin/xrag-server serve --data /absolute/parent/restored-xrag \
  --listen 127.0.0.1:8765 --workers off
```

先用恢复目录admin.key登录，读取旧引用并确认内容，再配置新的访问Key、来源路径、模型与构建。恢复数据目录存在RESTORE_INFO.json时，`demo.sh`和`run-linux.sh`不会像新演示库那样自动提交demo构建；直接serve默认也没有自动build参数。

## 4. 外部能力需要单独恢复

图和向量后端不在SQLite快照里，不能因为地址仍能连接，就认定它对应这份恢复数据。基础词法和已保存正文可以先运行。确认模型身份、外部库内容和来源后，再决定从保存资料/来源重建；缺失资料不能凭空恢复。

来源目录没有复制到备份。数据库中的旧路径只是登记信息：另一台机器可能不存在，或恰好有不同内容。不要在未检查前启用自动构建。restore不是跨域克隆产品，资料origin保留；并行运行旧、新副本时要明确命名连接，避免把恢复副本当原服务自动替换。

## 5. 升级与恢复不同

正常升级在同一个数据目录打开新程序，**保留旧Key及自动策略**。离线restore生成另一目录，主动停用旧运行身份和自动动作。升级回退使用升级前的完整停机备份，不直接用restore冒充“所有配置原样回去”。

当前没有在原运行库上的Web一键覆盖恢复、定时快照、对象存储备份、自动清理旧正文、加密备份包或生产RPO/RTO承诺。
