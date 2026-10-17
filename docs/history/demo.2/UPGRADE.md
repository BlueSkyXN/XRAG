> **历史归档**：本文保留原批次说明与当时验证记录。当前使用请阅读 [文档总览](../../README.md) 和 [当前状态](../../STATUS.md)。文中 checks 等报告路径可能仅存在于原执行环境。

# 从 0.7.0-demo.1 升级到 demo.2

本轮使用原始demo.1源码生成临时数据库，停止旧进程后启动demo.2，验证原Key、引用、已发布内容、来源和新评测功能，见 `checks/upgrade-smoke.json`。这不是读取或修改了用户电脑上的工作区。

## 建议步骤

1. 停止旧Server及**所有**同机worker；终端Ctrl+C后等待退出。只关闭浏览器不等于停止进程。
2. 将完整旧数据目录复制到新的备份位置；外部来源目录、providers配置和env/file秘密引用分别备份。不要用文件复制方式备份仍在写入的SQLite而漏掉WAL。
3. 新代码解压到新目录，不覆盖旧代码。编译新程序。
4. 指向原数据目录启动：

```bash
# 在新的源码目录中；路径替换为实际绝对路径
XRAG_DEMO_DATA=/absolute/path/to/existing/.xrag-demo bash demo.sh
```

脚本初始化发现已有Key时不会重建Key，也不会覆盖已有初始化数据；启动会为demo库提交增量构建。需要不自动构建：

```bash
make build
./bin/xrag-server serve --data /absolute/path/to/existing/.xrag-demo --workers off
# 需要构建时在另一终端使用同一数据目录
./bin/xrag-server worker --data /absolute/path/to/existing/.xrag-demo
```

数据库变更是新增表：source_config/source_policy/source_state/job_trigger/build_detail/eval_set/query_record及相应索引。文件/版本/ref格式、API Key摘要不改变。表缺失时显式启动数据库Open阶段创建，没有另一个后台迁移服务。

旧来源默认不开自动检查。首次完整成功构建后会填入新的来源状态；升级不会自动为所有目录开启轮询。

## 回退

停掉demo.2和worker，恢复升级前的完整数据备份，再用旧程序启动。不要假定旧程序能理解新建的git来源、eval任务和新状态；即使表结构是追加的，也不承诺新产生的数据可由demo.1直接继续写入。

源文件、模型输入缓存与历史版本会增长。本轮未做完整历史GC和限额管理，不用于无限持续导入。新查询摘要有独立条数和期限，不代表其它表也自动清理。

## 重跑本轮升级实验

```bash
make build
python3 scripts/upgrade_smoke.py --old-source /path/to/XRAG_V0.7_Demo_源码.zip
```

脚本在临时目录构建旧程序并创建测试数据，不接触现有运行库。它需要git不是为了升级，而是原测试运行环境已经具备git；构建必需Go/C/SQLite。归档原件不重复打入新源码包。
