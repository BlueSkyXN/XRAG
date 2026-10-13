# Demo.2 → Demo.3 升级

本轮用原始Demo.2 ZIP构建旧程序、创建临时数据，停止旧进程后用Demo.3打开同目录；检查11项行为，包括Key字节、旧引用、来源和新管理页面，实际记录见 checks/upgrade-smoke.json。未访问用户本地数据。

## 操作

1. 停止旧Server以及所有同机Worker；关闭浏览器不等于停止写入。
2. 备份完整数据目录；目录外来源、providers配置、凭据环境或文件分别保留。不要只复制打开的SQLite主文件而漏WAL。
3. 新源码解压到新目录，构建；不覆盖旧源码。
4. 指定原数据目录：

```bash
make build
./bin/xrag-server serve --data /absolute/existing/.xrag-demo --workers off
```

确认旧引用和资料可读，再启用执行器或另起worker。继续使用原admin.key，不会自动新建身份。

也可 `XRAG_DEMO_DATA=/absolute/existing/.xrag-demo bash demo.sh`，但演示脚本通常会提交demo的增量构建；不需要自动构建就用上面的serve。

## 数据变更

新增key_options、managed_setting、feedback_state、backup_record四张表，旧Key默认不设到期，旧反馈默认open/revision0。旧文件和ref格式不改；旧来源自动更新策略保留。启动打开时创建缺失表，不启动额外迁移服务。

未显式--config时：以前无模型的实例仍无模型；保存了managed settings的新实例会在启动时读取它。显式--config仍优先。Web保存只影响下一次启动；配置改动需重启Server和全部Worker，不能只重启浏览器。

## 回退

停止Demo.3及Worker，恢复升级前完整数据目录，用旧代码启动。**不建议旧版直接继续打开新库**：旧版不认识key_options里的到期设置，可能错误接受在新版已过期的Key；也不理解新管理配置。只因为是新增表，不能推导双向兼容。

## 与restore的区别

`restore --from ... --data 新目录`是恢复隔离副本，会禁用旧Key/自动动作并生成recovery-admin，不是保持所有运行设置的普通升级。详见DATA_RECOVERY.md。

## 重跑

```bash
make build
python3 scripts/upgrade_smoke.py --old-source /path/to/XRAG_V0.7_Demo2_源码.zip
```

脚本使用临时目录，不改原ZIP、不碰运行库。
