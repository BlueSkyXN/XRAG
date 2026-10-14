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

这个 Space 从 GHCR 镜像运行 XRAG，不再从源码构建。镜像由源仓库 `main` 的 CI 自动构建并推送；本 Space 的文件（README、Dockerfile、entrypoint.sh）由源仓库的部署 workflow 渲染并上传，不要在 Space 里直接改。

- 源仓库：<https://github.com/BlueSkyXN/XRAG>
- 运行的镜像：`ghcr.io/blueskyxn/xrag`（默认跟随 `:main` 标签，回退时按 digest 钉死；见 `Dockerfile` 的 `FROM` 与下文“更新与回退”）。

## 更新与回退

镜像由 GitHub Actions 构建并推送（PR 只测不推，`main` 只在全部冒烟通过后推送 `sha-<短SHA>` 和 `main` 标签）；HF 侧不依赖 Actions 的部署也能拿到新镜像。

**日常更新（跟随 :main，推荐）**：Space 的 Dockerfile 渲染为 `FROM ghcr.io/blueskyxn/xrag:main`（浮动标签）。镜像更新后，在 HF Space 页面点 **Rebuild**（或 Factory rebuild），构建器会拉取 `:main` 当前指向的镜像并重启——不需要动 GitHub Actions。前提：GitHub 侧 image workflow 已成功推送过 `main` 标签。

**经 Actions 部署（可指定版本）**：在 [Actions 页面](https://github.com/BlueSkyXN/XRAG/actions/workflows/publish-space.yml) 对 `main` 运行 `publish-space`：

- `image_digest` 留空：部署 `:main` 当前指向的镜像（workflow 会先解析出 digest 并钉死再上传）；
- 填 `sha256:...`：部署指定版本，用于回退或停在某个已知好版本。

Actions 部署会把 Space 文件重新渲染上传（含按 digest 钉死的 `FROM`）；要回到“跟随 :main”，再空跑一次 publish-space 即可。

- **回退**：用上一个 digest 再运行一次。同一个 digest 重复部署不会改变 Space 内容（workflow 会以“Space commit did not change”失败）；只想重启 Space 时请在 HF 页面操作，不要重跑部署。

workflow 会校验 digest 存在、渲染部署文件、上传到本 Space、等待重启，并以线上 `/healthz` 的 `commit` 与镜像的提交一致作为成功标准。

首次从整仓源码切换到镜像部署时，Space 里旧的源码文件会被删除（Space 的 Git 历史仍保留）。切换前按源仓库 `docs/DATA_RECOVERY.md` 备份 `/data/xrag`。

## 运行约定

- 登录密码由 Space secret 提供（`ADMIN_USERNAME`、`ADMIN_PASSWORD`），不写入仓库。
- 数据卷挂在 `/data`，服务数据在 `/data/xrag`，与镜像的默认路径一致。
- `/healthz` 是公开存活检查，返回版本和提交 SHA。
- Space 层入口（`entrypoint.sh`）检查 UID 1000 和 `/data` 挂载，按 HF 的 `PORT` 环境变量设置监听地址，并开启 `XRAG_TRUST_FORWARDED_PROTO`（HF 边缘终止 TLS，会话 cookie 由此带 `Secure`）。
