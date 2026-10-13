# Demo 范围与 V0.7 的差距

版本 0.7.0-demo.1。工程源自 V0.7 设计重新实现，不复用 V0.3 作为完成度证明。下面是**实际软件范围**，与完整设计不同。

## 已实现并在本环境运行

两个 Go 程序、Server 独立进程锁、可关闭的执行器、可另起同机 worker、SQLite WAL、MD/TXT 读取与上传、文件级 staging→active 发布、关键词 BM25、中文字符/双字词项 fallback、精确编号 token、目录/grep/原文读取、Unicode范围、固定 edition 引用、普通移除保留历史、明确撤回、范围句柄、缓存结果分页、精确关键词块总数、有界关系、实际步骤摘要、四角色/KB Key、知识草稿/显式发布、反馈、DB备份、local只读扫描、HTTP与MCP tools子集、实际工作台。

图基础版使用保存在 SQLite 文件版本中的**明确标注关系**，不是假装连接了 Neo4j，也不是自动识别问题；允许在没有外部数据库的情况下测试路径、出处和范围规则。关系语法 `[[A]] --使用--> [[B]]`。本演示图与 V0.7 的专业 Neo4j 部署目标有区别。

## 代码已存在，仅使用受控端点测试

Embedding HTTP、Rerank HTTP、Wiki Chat、Qdrant REST、Neo4j Query API、direct只读Qdrant/可选Neo4j。模型测试输出明确使用fixture，不进入默认运行配置。真实模型效果、Neo4j Cypher 执行、Qdrant tokenization/查询性能、商业客户端兼容性没有实测。

## 有意缩小或替代的部分

| V0.7 设计 | 本 Demo |
|---|---|
| gse / goldmark / YAML | 无网络依赖：Han 字符和双字词项、保守标题分块、JSON配置；frontmatter只支持单行标量/数组，非完整YAML或CommonMark |
| NFC与复杂块segments | 只做UTF8/BOM/换行规范化；code-point定位，无NFC及表格重排 |
| 全部来源和五触发 | path/builtin＋手动/API/可选周期；Git、OSS、WeKnora、Hook、fsnotify未实现 |
| 模型可选投影部分发布 | 未配置可以不用；配置了模型/外部后端后，对单文件采用required，失败保留旧版；不悄悄降级发布 |
| 图抽取 | 默认显式标注；可选JSONL抽取器，未提供默认LLM自由抽取器 |
| 向量代号发布迁移 | 物理集合按模型语义身份固定，不实现alias和整KB切换；换模型重建期间不匹配明确报错 |
| 专业后端维护 | 外部staging/retired尚无完整补偿GC；Server按edition过滤，direct会看到未清理版本 |
| direct完整性 | 仅本Demo物理集合格式，需要Qdrant；没有Neo4j-only读取组合、任意已有库映射、旧设计格式迁移 |
| Wiki大纲+正文+改稿+同步 | 一次固定生成调用，最多12块；生成草稿并检查编号存在，语义支持需人判断；不做自动主题推荐/旧主题重生成/来源写回 |
| 依赖管理 | 同KB文档/块引用；反向依赖保守并集；知识草稿不接受聚合/路径作为发布依据；源变更needs_review，撤回blocked |
| 结果/引用保留与GC | 临时结果10min、handle24h；历史文件版持续保留，尚无限额GC；不能无限生产导入 |
| 持久预算 | 保存调用attempt/known/unknown；每job最多200次外部调用＋排队/执行期限；未做完整token/货币预留账务 |
| 任务恢复 | 同KB互斥，过期租约失败并标未知调用；显式重提，不自动续跑 |
| MCP标准SDK | 自写固定2025-06-18 tools子集；input/output schemas，非全协议 |
| 评测台 | 有真实过程JSON、代码/过程测试；未完成题集管理/A-B评测界面 |
| 配方与Research Agent | 未实现，符合最新V0.7 M6预留；+ask不生成答案 |

## 不可夸大的事实

- 单元与模拟HTTP测试通过不等于真实数据库联合运行通过。
- DOM交互＋HTTP桥接通过，不等于浏览器直接导航部署网址通过。
- 程序可运行不等于适合生产负载、公开互联网或多机器共享。
- 代码能在当前Linux编译，不等于macOS已实机验证；提供本机构建脚本。
- 本轮没有独立子代理执行结果，不以自审冒充独立评审。
- 本轮没有读取/修改用户本地Git仓库，没有替用户选择发布许可证。
