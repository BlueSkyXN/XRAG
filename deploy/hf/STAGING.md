# Staging Space 验证

在改变镜像、入口、卷挂载或存储相关配置前，用独立 Space 和测试数据验证，再把同一 digest 部署到目标环境。历史会话中的一次性“跳过 staging”决定不作为长期部署规则。

## 隔离与前提

使用独立 Space、独立可写持久卷和测试登录凭据。HF 入口要求 UID 1000 与 `/data` 挂载，测试也遵守这两个条件。不要让两个进程共享同一个可写 SQLite 目录。

若确实使用只读挂载并将 `XRAG_DATA_DIR` 指向 `/tmp/xrag-staging`，应用读写的是新临时库，检索也只读该库的测试资料；不会自动读取挂载卷中的另一份数据库。`/tmp` 数据不覆盖重启持久性验收，因此常规 staging 使用独立持久卷更合适。

## 准备目标

1. 创建 Docker、cpu-basic 的测试 Space，例如 `your-account/xrag-staging`；按账号权限选择可见性。
2. 为测试 Space 挂载独立持久卷到 `/data`，确认 UID 1000 可写。卷内准备按现有部署要求处理。
3. 配置测试 `ADMIN_USERNAME`、`ADMIN_PASSWORD`，可选 `XRAG_BUILD_ON_START`。凭据不要写入仓库或 workflow 日志。
4. 确认 GHCR 镜像公开，选定 image workflow 通过后的 digest，并记录待验收 commit。

创建/挂载通过 HF UI 或本机 `hf` 当前版本提供的对应命令完成；CLI 参数以 `hf spaces --help` 与相关子命令帮助为准。创建硬件或卷可能涉及配额和费用，使用已有明确选定的测试资源。

## 部署同一镜像

在 [publish-space](https://github.com/BlueSkyXN/XRAG/actions/workflows/publish-space.yml) 的 `main` 上填写：

```text
space: your-account/xrag-staging
image_digest: sha256:<selected-image-digest>
```

workflow 会替换目标 Space 仓库中的部署文件，等待重建并检查 commit。部署完成后可只读检查平台状态：

```bash
hf spaces info your-account/xrag-staging --json
```

渲染后的 Space 仓库只包含部署文件，原始应用源码来自 GHCR。更新和回退见 [README.md](README.md)。

## 验收顺序

| 检查 | 成功标志 |
|---|---|
| 平台入口 | UID、挂载与 PORT 检查通过，实例进入运行状态 |
| 版本 | `/healthz` commit 与所选镜像的 revision 一致 |
| 登录 | 正确测试凭据可登录，无身份请求受保护接口返回 401 |
| HTTPS Cookie | 会话 Cookie 含 Secure；退出后会话失效 |
| 示例检索 | demo 构建结束，关键词和原文引用返回实际测试资料 |
| 写入 | 上传带唯一标识的测试 MD，构建成功并可检索 |
| 持久性 | 重启 Space 后仍能检索刚上传的唯一标识并读取原引用 |
| Worker/任务 | 任务从 queued 到终态，取消和失败详情可见 |
| 恢复（本次涉及时） | 在独立恢复目标验证快照和引用；原 staging 数据保持可回查 |

`/tmp` 模式只能检查临时实例的运行行为。上传后重启丢数据是该模式的预期，但不能据此通过持久卷验收。外部模型/数据库仅在本次涉及的配置下单独验收并记录费用与版本。

## 部署与回退记录

保留 digest、commit、测试 Space、卷身份、执行日期、检查结果及失败日志。检查通过后，在正式目标使用相同 digest；仍需核对正式环境的登录、数据和连接配置。

清理前逐项确认测试 Space 与独立测试卷身份，保存需要的报告和数据，再按平台工具移除。删除 Space 与删除卷是不同操作；不要把删除测试实例扩展到共享或正式存储。
