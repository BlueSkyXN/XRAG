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
- 运行的镜像：`ghcr.io/blueskyxn/xrag`，按 digest 固定（见 `Dockerfile` 的 `FROM`）。

## 更新与回退

部署和回退都通过 GitHub Actions 的 `deploy-hf` workflow 手动触发：

- **更新**：在 [Actions 页面](https://github.com/BlueSkyXN/XRAG/actions/workflows/deploy-hf.yml) 对 `main` 运行 workflow，输入要部署的镜像 digest（`sha256:...`，来自 image workflow 的 job summary）。
- **回退**：用上一个 digest 再运行一次。

workflow 会校验 digest 存在、渲染部署文件、上传到本 Space、等待重启，并以线上 `/healthz` 的 `commit` 与镜像的提交一致作为成功标准。

首次从整仓源码切换到镜像部署时，Space 里旧的源码文件会被删除（Space 的 Git 历史仍保留）。切换前按源仓库 `docs/DATA_RECOVERY.md` 备份 `/data/xrag`。

## 运行约定

- 登录密码由 Space secret 提供（`ADMIN_USERNAME`、`ADMIN_PASSWORD`），不写入仓库。
- 数据卷挂在 `/data`，服务数据在 `/data/xrag`，与镜像的默认路径一致。
- `/healthz` 是公开存活检查，返回版本和提交 SHA。
- Space 层入口（`entrypoint.sh`）检查 UID 1000 和 `/data` 挂载，按 HF 的 `PORT` 环境变量设置监听地址，并开启 `XRAG_TRUST_FORWARDED_PROTO`（HF 边缘终止 TLS，会话 cookie 由此带 `Secure`）。
