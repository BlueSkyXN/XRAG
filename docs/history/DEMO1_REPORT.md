> **历史归档**：本文保留原批次说明与当时验证记录。当前使用请阅读 [文档总览](../README.md) 和 [当前状态](../STATUS.md)。文中 checks 等报告路径可能仅存在于原执行环境。

# XRAG 0.7.0-demo.1：本轮实现与运行检查

**2026-10-11｜本轮真实源码与真实运行结果。不是生产能力结论。**

## 交付结论

新增两个 Go 程序，实际 Web 工作台、SQLite 知识构建/查询、Client local/server、可选专业后端/模型适配。按 V0.7 做可运行 Demo，未宣称完成 V0.7 全套设计；完整差距见 `docs/STATUS.md`。

## 实际测试

| 检查 | 本轮结果 | 原始记录 |
|---|---|---|
| Go 单元/集成/协议适配测试 | **50 个顶层测试通过** | go-test.log |
| Go race 检查 | 通过 | go-race.log |
| go vet | 通过，无报告 | go-vet.log |
| 全包语句覆盖率 | **60.2%**，不代表功能完成率或检索效果 | go-cover.log |
| 真实 Server/Worker/Client/MCP 进程与 HTTP 场景 | **24 项通过** | process-smoke.json |
| DOM 交互、响应式与实际 HTTP 桥接 | **10 项通过** | process-smoke.json、截图 |
| 源码重新解包构建/测试、Linux程序重新解包运行 | 见 package-check.json，必须以该文件实际结果为准 | package-check.json |

## 程序运行覆盖

无模型启动；API Key；未知参数；API关闭执行器时构建保持queued；独立worker完成实际文件构建；37条结果10/10/10/7分页不重查；F1范围不带入F2关系；两跳路径；原文引用；语义缺失明确错误；空HOME远程Client不建资料库；dry-run无执行；草稿隐藏、发布后搜索；HTTP/stdio MCP实际二进制；Wiki缺Chat报错；文件上传；worker关闭后继续查询；取消；Server停止后local扫描；local无持久数据库。

## 语义与数据测试

UTF8/中文短词/精确编号、rune位置、内容键与edition分离、pending块不可查询、历史原文可读、普通移除保留历史、明确撤回使结果失效、分页精确总数、结果游标不能串用、within约束关系支撑、显式scope.expand、关系方向与结果ref、关系幂等、草稿发布原子切换、草稿编辑不覆盖已发布正文、依赖撤回阻止条目、同KB多句柄领取互斥、请求幂等、取消阻止发布、未知调用持久化、范围与身份、来源扫描失败不删除、metadata更新复用真实模型输入缓存、备份恢复等。

## 模型与专业后端：必须单独理解

- Embedding/Rerank/Chat接入为真实HTTP代码。测试使用本地脚本化HTTP端点，返回中明确标fixture，**不是实际大模型效果实验**。
- Qdrant REST写入/过滤/查询/元信息、Neo4j Query API的请求格式和202错误检测、direct引用/模型绑定，使用HTTP测试服务验证。**没有实际启动Qdrant/Neo4j。**
- JSONL抽取器执行了真实Python子进程；测试的是明确文本标注，不是智能抽取质量。
- 默认应用不包含或启用假Embedding、假Wiki、假数据库返回。
- 未取得商业模型凭据，未做语义相关性、图提升幅度或中文Qdrant文本过滤效果评测。

## 浏览器实际限制

受当前 Chromium 管理策略影响，直接导航 `http://127.0.0.1` 返回 `ERR_BLOCKED_BY_ADMINISTRATOR`。没有修改或绕开该策略。

本轮将项目自身HTML、CSS和JS装载到空白页面，通过明确的测试函数将页面API请求发往已实际运行的HTTP服务。测试函数只在测试页面注入，**不在交付应用中**。HTTP服务另外已由独立客户端与真实进程测试。

由此验证了：页面登录交互、搜索显示37总数、10条卡片、读取原文对话框、两跳关系、资料列表、估算返回、已发布知识、能力状态、390px无全页横向溢出、无JS运行错误。该方式**不证明真实部署下浏览器网络、Cookie、CORS、代理或TLS完全可用**。Cookie服务本身通过独立HTTP会话验证；真实浏览器直接打开需本机再测。

截图 desktop-search.png、desktop-graph.png、desktop-knowledge.png、mobile-search.png 来自上述真实DOM与服务数据，合成资料不是真实公司数据。

## 本轮修正的问题

1. 缓存期限有数据库列和JSON两处，读取改以数据库列为准，增加到期不回退全库测试。
2. Table输出原先打印count指针，已改成实际数值。
3. Client profile分支自赋值被vet发现，已修正。
4. file_hash改用规范化文本原始字节SHA-256，不使用JSON字符串摘要。
5. 构建有失败文件时标partial，不用succeeded掩盖局部失败。
6. 知识发布事务内再次检查依赖撤回，避免准备后到提交前的变化。
7. Cookie会话增加服务端期限；Reader不能进入草稿管理接口。
8. direct继承descriptor的origin后必须让向量适配使用同一origin，已增加模拟协议测试。
9. 缺向量数据显式报错，不以空结果冒充语义检索正常。
10. Wiki引用编号检查覆盖所有编号，仍不把“编号合法”称为“结论正确”。

## 环境与未做事项

Go 1.23.2、Linux x86_64、Debian 13、GCC、系统 SQLite 3.46.1/FTS5、Python 3.13、Chromium、Playwright。Server通过CGO连接系统SQLite；Client实际以CGO_ENABLED=0构建。未下载第三方Go模块，容器DNS无法访问外部下载源。

没有macOS/Windows实机测试、Docker构建、真实商业Agent联调、生产数据压测、多机器运行或自动恢复试验。原生macOS需要在用户机器编译，脚本已提供但未实机验证。

本轮无可调用的独立子代理执行器；以上为同一实现者的自动测试与自查，未声称独立子代理评审通过。没有读取或修改用户本地仓库，也没有替用户改变LICENSE。
