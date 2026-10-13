# XRAG Demo.9 实际范围

`0.7.0-demo.9` 是全新安装开发版，存储标记 demo9。不迁移旧软件目录，不修改用户本地仓库。当前文件更新、固定引用与同版恢复仍属于正常产品能力。

## 本轮实际新增

1. MCP POST JSON/SSE、通知202、明确版本协商、在途取消、stdio有限并发；静态指南/可见目录/固定引用资源和检索提示。不同入口调用相同应用操作。
2. 可选外部 OAuth 资源服务：RFC7662 token introspection、明确issuer/audience/expiry/subject、scope与角色/库范围交集、RFC9728元数据与challenge。不是授权登录服务器。
3. Research/Recipe持久检查点与显式resume：固定原范围、配置、预算和截止时间；已存规划/结果复用；未知付费调用拒绝重放；同一逻辑任务再次领取，不创建替身。
4. 任务SSE快照订阅、CLI watch、工作台实时查看与继续按钮；断开查看不取消任务，长连接不占内容租约。
5. 纯Go Client跨平台构建与独立压缩包；不包含对应平台Server。
6. 修正任务撤回显示中的一次新回归：不可在加入检查点摘要时重新覆盖已经隐藏的依据。新增测试并重跑原撤回场景。

## 实际验证层次

运行过的是本机系统SQLite、Go Server/Worker/Client、真实HTTP/SSE、stdio、资料和Recipe操作、临时SQLite写故障后的续跑、既有构建/检索/维护等流程。

OAuth身份服务使用本地受控RFC7662端点；Research规划和Qdrant/Neo4j/S3/WeKnora仍为受控协议测试。没有真实第三方身份服务或Agent客户端结果。浏览器直接导航被环境策略阻止时，才使用自身页面和显式HTTP桥接；SSE界面测试接收真实已终止流的body，增量帧/取消由Go与CLI另测，不冒称完整部署E2E。

四平台Client交叉编译不是四平台实机；Linux完整程序包单独解包运行。没有Docker运行、商业模型质量或真实业务容量结论。

## 尚未完成／当前边界

- 真实专业后端、真实模型和direct联合运行；真实语料的中文检索、抽图、Wiki/Research质量与成本比较；S3/WeKnora/厂商Hook/远程Git现场。
- 复杂direct、范围元数据、图前沿、全文grep/固定View评测仍有上限或较多数据装载；没有无限容量或生产并发承诺。
- 在线内容GC会等待或阻挡内容操作；没有多机/direct读者租约；孤儿产物自动发现未做，整空集合仍显式离线回收。
- 投影恢复只恢复SQLite已保存的XRAG产物，不是供应商全实例联合快照、原来源或凭据备份。
- 续跑是有边界、明确发起；未知付费结果不自动重放。Recipe不扩成通用条件/循环/业务写工具。
- MCP支持所列2025修订，不是完整2026新线；无sampling/Tasks/subscriptions、完整会话重放或自建OAuth登录/刷新客户端。商业IdP和外部Agent仍需联调。
- Mac/Windows Server实机与原生事件、Docker、真实代理/浏览器部署未完成；新增Client包不等于Server移植。
- 插件市场/池/沙箱、任意图模式自动识别、强制盲评/语义重复题、PDF/Office/OCR/任意业务数据库未扩大到本轮。

代码存在、协议测试、真实部署和业务效果分开报告。详细过程与结果见 checks/REPORT.md。
