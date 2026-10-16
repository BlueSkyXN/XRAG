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

这个 Space 从 GHCR 镜像运行 XRAG。镜像由源仓库 `main` 和版本 tag 的 CI 构建并推送；本 Space 的文件（README、Dockerfile、entrypoint.sh）由源仓库的部署 workflow 渲染并上传，修改这些文件应在源仓库完成后重新部署。

- 源仓库：<https://github.com/BlueSkyXN/XRAG>
- 运行的镜像：`ghcr.io/blueskyxn/xrag`，实际部署版本以 Space 的 `Dockerfile` 中固定的 digest 为准。

## 更新与回退

镜像通过容器冒烟后才会推送；PR 只执行构建和检查。`main` 发布 `sha-<短SHA>` 与 `main` 标签，版本 tag 发布对应的版本镜像。

**更新**：确认 image workflow 成功后，在 [Actions 页面](https://github.com/BlueSkyXN/XRAG/actions/workflows/publish-space.yml) 对 `main` 运行 `publish-space`：

- `image_digest` 留空：部署 `:main` 当前指向的镜像（workflow 会先解析出 digest 并钉死再上传）；
- 填 `sha256:...`：部署指定版本。正式发版时填写该版本 tag 构建输出的 digest，回退时填写上一个可用版本的 digest。

每次 Actions 部署都会固定 digest。之后点击 HF 的 **Rebuild** 会重建该固定版本；要更新到新的 `main` 镜像，需再次运行 `publish-space` 并留空 `image_digest`。

- **回退**：用上一个 digest 再运行一次。同一个 digest 重复部署不会改变 Space 内容（workflow 会以“Space commit did not change”失败）；只想重启 Space 时请在 HF 页面操作，不要重跑部署。

workflow 会校验 digest 存在、渲染部署文件、上传到本 Space、等待重启，并以线上 `/healthz` 的 `commit` 与镜像的提交一致作为成功标准。

首次从整仓源码切换到镜像部署时，Space 里旧的源码文件会被删除（Space 的 Git 历史仍保留）。切换前按源仓库 `docs/DATA_RECOVERY.md` 备份 `/data/xrag`。

## 运行约定

- 首次启动会导入示例并构建 `demo` 库；HF 默认设置 `XRAG_BUILD_ON_START=demo`，已有空库会在下次启动时构建。可将该变量设为其他 KB ID，以便每次启动构建指定库；恢复副本（数据目录有 `RESTORE_INFO.json`）跳过自动构建。demo 库需要登录后使用。
- 登录密码由 Space secret 提供（`ADMIN_USERNAME`、`ADMIN_PASSWORD`），不写入仓库。
- 数据卷挂在 `/data`，服务数据在 `/data/xrag`，与镜像的默认路径一致。
- `/healthz` 是公开存活检查，返回版本和提交 SHA。
- Space 层入口（`entrypoint.sh`）检查 UID 1000 和 `/data` 挂载，按 HF 的 `PORT` 环境变量设置监听地址，并开启 `XRAG_TRUST_FORWARDED_PROTO`（HF 边缘终止 TLS，会话 cookie 由此带 `Secure`）。
