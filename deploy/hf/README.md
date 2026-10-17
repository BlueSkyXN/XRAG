---
title: XRAG
emoji: 🔎
colorFrom: green
colorTo: gray
sdk: docker
app_port: 8765
suggested_hardware: cpu-basic
pinned: false
license: gpl-3.0
---

# HF Space 部署

此 Space 从 `ghcr.io/blueskyxn/xrag` 镜像运行 XRAG。源仓库的 image workflow 构建并检查镜像，publish-space workflow 渲染 README、Dockerfile、entrypoint.sh 后上传。修改在 [源仓库](https://github.com/BlueSkyXN/XRAG) 完成，再重新部署。

实际运行版本以 Space Dockerfile 固定的 digest 与 `/healthz` 的 commit 为准。产品使用说明见 [项目文档](https://github.com/BlueSkyXN/XRAG/blob/main/docs/README.md)。

```mermaid
flowchart LR
    Code["源仓库提交"] --> CI["image 构建与容器冒烟"]
    CI --> Image["GHCR 镜像 digest"]
    Image --> Deploy["手动 publish-space"]
    Deploy --> Files["固定 digest 的 Space 文件"]
    Files --> Build["HF 重建并启动"]
    Build --> Check["healthz commit 与镜像一致"]
```

## 部署前提

- 目标为 Docker Space，配置独立、可写的持久 `/data` 挂载；服务数据在 `/data/xrag`。
- 镜像包公开，HF 可匿名拉取。workflow 读取 GitHub Packages visibility，非 public 或不可读取时停止。
- GitHub Actions 已配置有权更新目标 Space 的 `HF_TOKEN`；登录所需 `ADMIN_USERNAME`、`ADMIN_PASSWORD` 放在目标 Space 的变量/Secrets 中，密码至少 16 字符。
- 记录当前 digest，按 [数据恢复指南](https://github.com/BlueSkyXN/XRAG/blob/main/docs/DATA_RECOVERY.md) 备份需要保留的数据。多个 Space 不共享同一可写 SQLite 数据目录。

## 更新与回退

确认 image workflow 成功，在 [publish-space Actions](https://github.com/BlueSkyXN/XRAG/actions/workflows/publish-space.yml) 选择 `main`，填写：

| 输入 | 含义 |
|---|---|
| `space` | 目标 owner/name，默认 BlueSkyXN/XRAG；部署前核对目标 |
| `image_digest` 留空 | 将 `:main` 当前指向的镜像解析为 digest 后固定 |
| `image_digest=sha256:...` | 部署指定镜像；发布/回退使用已记录的对应 digest |

workflow 校验镜像与 commit，渲染部署文件，上传并等待 Space commit 改变和重建，最后核对 `/healthz` 的 commit。

上传使用 `--delete "*"` 同步 Space 仓库：渲染目录之外的仓库文件会被移除，Git 历史仍保留。持久卷另行管理。首次从源码部署切换或 Space 仓库中另有文件时，先确认这些文件的保留方式。

HF **Rebuild** 重建当前固定版本。更新 `main` 镜像需要重新运行 publish-space。回退则填写上一可用 digest；软件回退是否兼容已有数据需按存储版本判断。

若渲染文件没有变化，重复部署会因 “Space commit did not change” 失败。只需重启时使用 HF 的重启操作。健康检查证明程序提交与预期一致，登录、检索、持久性和外部模型仍应执行对应验收。

## 运行约定

- UID 必须为 1000，`/data` 必须为挂载点；入口不满足时拒绝启动。
- `PORT` 默认 8765，包装入口据此设置监听地址。
- 默认 `XRAG_BUILD_ON_START=demo`，每次启动排队构建 demo；可设置为其他有效 KB ID。初始化导入的示例需登录使用。
- 恢复副本含 `RESTORE_INFO.json` 时跳过自动构建；确认来源路径后手动构建。
- HF 边缘终止 TLS，包装入口启用 `XRAG_TRUST_FORWARDED_PROTO`，HTTPS 会话 Cookie 带 Secure。
- `/healthz` 公开返回版本与 commit；它不验证数据完整性或模型可用性。

重大部署变化建议先在独立 staging Space 验证。源码仓库中的 staging 指南说明测试卷、登录、读写、重启持久性与清理边界。
