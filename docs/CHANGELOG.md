# 未发版（0.8.0-exp.1 之后）

## 部署与镜像
- 镜像通用化：不再内置 nginx，`xrag-server` 直接监听 `0.0.0.0:8765`（`XRAG_LISTEN` 可覆盖），上传上限与超时报错回归应用自身；通用入口见 `docker/entrypoint.sh`，数据目录 `XRAG_DATA_DIR`（默认 `/data/xrag`）。
- 新增 `image` workflow：push `main`/tag 自动构建并推送 `ghcr.io/blueskyxn/xrag`（`sha-<短SHA>`、`main`、版本标签，无 `latest`），推送前跑两轮镜像冒烟（挂载+密码、不挂载+API Key）。`ci` workflow 只保留测试。
- `/healthz` 增加 `commit` 字段（构建时注入的提交 SHA）；镜像带 `org.opencontainers.image.revision` label。
- HF front matter 与部署说明移至 `deploy/hf/README.md`；主 README 改为项目说明与 Docker 运行指南。

## 工作台
- 新工作台取代经典界面，挂在 `/`；页面路径（如 `/search`、`/settings/keys`）由 Server 回落到 `index.html`，缺失的静态文件仍返回 404，带哈希的资源长期缓存。
- 覆盖经典界面的全部功能：检索、关系、调查、资料与来源、任务、知识与 Wiki、反馈、评测、概览、接入、设置。

## 接口
- `files.read` 新增 `context_lines`（0–500）。对文本块和 grep 匹配引用，返回 `data.window`：引用所在文件版本中的整行上下文，以及引用行区间 `cite_start/cite_end`；行号含 front matter。不改变原有 `text` 字段。
- `+ask` 的结果项新增 `routes`，列出找到该结果的检索路线（`keyword`、`semantic`、`graph`、`external`）；RRF 融合时取并集，翻页读取保存结果时保留。单一路线动作不加这个字段。
- `GET /v1/jobs` 支持 `kb` 与 `kind`（逗号分隔）筛选。

## 部署
- 合入 `main` 不再自动更新 HF Space。需要部署时，在 GitHub Actions 里对 `main` 手动运行 `ci` workflow；测试和镜像检查通过后才会上传。

## 检查
- 新增 `scripts/smoke_workbench.py`（HTTP 契约；`--browser` 驱动工作台页面）与共用的 `scripts/workbench_browser.py`；`smoke_demo9.py --browser` 改为检查新工作台。demo.8 及更早批次脚本的 `--browser` 针对已移除的经典界面，不再维护。

# 0.7.0-demo.7（全新安装）

新增 compact 查询用元数据、流式本地精确向量Top-K、图邻接与总量限制、引用式结果延迟正文装配；离线退休向量集合登记与清理；GitHub/GitLab/Gitea Push 接收；修复 PruneExternal 重复拼接 Qdrant 集合名。真实后端仍未运行；没有迁移旧软件数据。

以下是历史版本记录，不代表本轮所有历史结论重新进行了对应现场验证。

# 0.7.0-demo.4

- Wiki同主题再生成、两阶段固定生成、修订列表和archive。
- 本机目录/Git专用分支写回、幂等冲突和精确副本识别。
- S3 SigV4完整分页/条件读取；WeKnora文档分块文本导入。
- 离线本地历史prune和运行锁。
- 修复direct物理集合检查；增加真实后端联调脚本（本轮未运行真实服务）。
- 保留原两个程序及M6边界，不改变许可证。

# 0.7.0-demo.3

日期：2026-10-12（会话日期）。

## 新增
- 使用概览与任务式上手。
- 持久模型/后端配置，重启生效、外部文件优先。
- Key列表、命名、到期、停用、退出会话；仍是Key级身份。
- 反馈表单、处理状态、分页、原引用阅读。
- 已发布资料导出、数据库快照管理、新目录离线restore、过期缓存清理。
- CLI对应操作、profile管理、只写新文件输出；实际帮助页。

## 修正
- 手机侧栏产品样式覆盖移动端关闭状态，已调整并实测开关。
- Key创建JSON显式空范围不再被Client默认KB覆盖。
- 旧明文profile字段在列表中遮蔽，明确未知profile不自动回退。
- 原MCP/检索/来源逻辑不被管理页面复制。

## 边界
- 没有完整账号目录、全部历史GC、自动备份、模型热切/真实后端联调。
- 新旧数据库升级与离线恢复是不同操作。
- 原Demo.2历史说明保存在docs/history/demo.2；本轮结果见checks/REPORT。

---

# 0.7.0-demo.2

日期：2026-10-12（会话日期）；不是完整V0.7完成声明。

## 新增
- `internal/source` 全量来源快照；本机Git固定commit/tree/blob读取，公共HTTPS mirror分支。
- 每来源定时强指纹检查、目录轮询和去抖；自定义签名Hook v1；触发统一入KB构建队列。
- 来源最近检查/成功状态、构建逐文件细节和触发原因。
- 人工题集导入、显式策略对比、Hit/Recall/MRR、负例指标、配对遗漏和逐题实际过程。
- 查询过程持久化、CLI查看；界面新增检索评测、来源与自动检查、任务详情。
- 编辑已有知识草稿的界面；原发布语义不改变。
- additive数据升级检查、Hook发送脚本、操作和升级文档。

## 修正
- 来源变更列表不再与全目录缺席删除混用；失败扫描不移除旧资料。
- 已撤回的源路径不会重新导入，也不使健康来源永远反复重试。
- 排队full构建不被后来的incremental覆盖。
- CLI实际支持 `--direction`；管理命令的KB选项不再成为非法JSON字段。
- 新界面长任务ID在窄屏换行，表格保持局部滚动。
- 查询过程与评测结果在相关撤回版本变化后不再返回旧派生内容。

## 不在本轮
- OSS/WeKnora来源、私有HTTPS/SSH Git凭据、Git LFS/子模块、系统事件监听、cron表达式、云平台原生Hook。
- Research/Recipe、全文档解析升级、完整GC、完整token/货币预算、多机器运行。
- 真实Qdrant/Neo4j/商业模型联调或效果结论；Mac/Windows实机测试；独立子代理评审。

旧版状态和测试在 `docs/history/`，不与本轮结果混用。软件仍两个Go程序，`+ask`仍为检索组合。
