> **历史归档**：本文保留原批次说明与当时验证记录。当前使用请阅读 [文档总览](../../README.md) 和 [当前状态](../../STATUS.md)。文中 checks 等报告路径可能仅存在于原执行环境。

# XRAG 0.7.0-demo.3：实际开发与检查

会话日期2026-10-12；宿主时钟另见environment.json。基线为Demo.2源码ZIP，本轮在其上修改，不以旧报告证明新功能。

## 本轮交付

使用概览、模型/后端保存配置、应用Key管理、反馈处理、已发布资料导出、SQLite快照管理及新目录离线恢复、过期缓存清理、CLI配套命令、帮助页。未改变两个程序、Server建设/Client读取和+ask不生成答案的定位。

## 实际结果

| 检查 | 结果 | 文件 |
|---|---:|---|
| Go顶层测试 | 94通过，0失败（原71＋新增23） | go-test.jsonl / test-summary.json |
| Race | 通过 | go-race.log |
| go vet | 通过 | go-vet.log |
| 全包语句覆盖率 | 62.9% | go-cover.log |
| 原基础流程 | 24实际进程/HTTP＋10页面 | process-smoke.json |
| 原Demo.2增量流程 | 24实际进程/HTTP＋8页面 | demo2-smoke.json |
| 本轮新增流程 | 28实际进程/HTTP＋16页面 | demo3-smoke.json |
| Demo.2→Demo.3升级 | 11通过 | upgrade-smoke.json |
| 重新解包验证 | 见package-check.json实际结果 | package-check.json |
| 请求Schema与示例 | 实际数量见management-contracts.json | management-contracts.json |

程序/HTTP共76项，页面共34项。每项是脚本的具体断言，不是独立完整业务流程数或检索效果评分。语句覆盖率不是功能完成率。Go记录及三份过程JSON保存本轮实际运行，不把历史图像结果算进来。

## 新增测试覆盖

Key列表不含token/hash、过期拒绝、范围/期限、最后永久Admin、停用后会话失效、logout；配置仅保存不调用、修订冲突、外部文件优先、明文拒绝；反馈分页/主体范围/处理修订/撤回清除说明；发布导出不含草稿；管理角色；快照下载删除；恢复固定ref但停用旧运行设置；拒绝覆盖和非数据库；过期缓存范围；overview；profile改名；未知profile；文件下载拒绝覆盖；CLI明确空Key范围不被默认KB替代。

源码中 tests/demo3_test.go 有23个顶层测试。真实进程另验证保存配置后重启生效（端点故意不可用，没有模型调用）、旧Key与新恢复Key不同、恢复副本读取原ref、CLI二进制下载、内容ZIP及完整校验摘要。

## 浏览器边界

Chromium实际导航localhost返回ERR_BLOCKED_BY_ADMINISTRATOR。本轮不改策略，用项目自身HTML/CSS/JS装载空页面，明确的Python测试桥接把fetch请求发送给实际运行Server。桥接仅在检查脚本里，不在交付应用代码中。截图来自合成资料和真实服务数据。

由此验证界面登录、反馈表单/处理、Key创建/隐藏、保存配置、真实备份列表、过期缓存预览、帮助、手机布局和导航、退出清空旧数据、Reader控件范围。**不等于用户部署网址的浏览器网络、TLS、代理、CORS与下载系统界面已验证。**CLI下载内容和快照恢复另外由真实HTTP/进程验证。

## 发现后修正

1. 新sidebar flex样式覆盖了手机隐藏规则：旧流程回归发现菜单被侧栏挡住，已修正媒体规则并新增关闭→打开→选择后关闭检查。
2. 旧页面检查器未装载新增product.js，更新测试装载清单；测试器更新不当作产品功能。
3. CLI管理命令原来移除kbs会使Key创建的显式空范围被默认KB替代，已保留创建动作数据并添加反例。
4. restore将已发出但未完成的调用按实际存储状态sent改为outcome_unknown，不虚构已结算。
5. Key列表与profile列表不导出秘密正文；配置只读外部文件时不反写运行设置。

## 升级与恢复

升级使用原Demo.2 ZIP在临时目录编译旧程序并产生真实资料；新版本打开同目录，原Key字节、文件ref和内容保持，旧来源不开新自动动作，新管理接口可用，再开一次仍保持。

恢复是另一个操作：新目录、旧资料origin/ref保留、新恢复Key、旧Key/自动扫描/managed provider/未完成任务禁用。恢复测试不修改原目录，也不代表外部目录和后端被备份。

## 未完成范围

真实Qdrant/Neo4j/模型联合运行、Mac/Windows实机、公开站点浏览器E2E、完整账号目录、数据历史GC、定时/远端备份、OSS/WeKnora、Research/Recipe、完整模型账务、多机或生产性能，均未声明完成。仍为单机SQLite Demo。只有自动测试与同一实现者自查，无独立子代理报告。

本轮未访问用户本地仓库/数据，没有替用户修改许可证。新管理请求Schema属于文档+输入样例；运行逻辑同时由Go字段和业务判断执行，不声称引入了通用Schema运行平台。

## 重跑

```bash
make build && make test && make race && make vet
python3 scripts/smoke.py --browser
python3 scripts/smoke_demo2.py --browser
python3 scripts/smoke_demo3.py --browser
python3 scripts/upgrade_smoke.py --old-source /path/to/XRAG_V0.7_Demo2_源码.zip
python3 scripts/check_management.py
python3 scripts/package.py --out /tmp/xrag-release
python3 scripts/check_release.py --source /tmp/xrag-release/XRAG_V0.7_Demo3_源码.zip --linux /tmp/xrag-release/XRAG_V0.7_Demo3_Linux_amd64.tar.gz
```

Python页面检查需要Playwright/Chromium，Schema检查需要jsonschema；这些不是默认运行Server的依赖。包最终仅重新加入检查结果文档，不改变已测试的产品源码/二进制。
