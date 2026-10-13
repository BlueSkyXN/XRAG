---
title: XRAG
emoji: 🔎
colorFrom: green
colorTo: gray
sdk: docker
app_port: 7860
suggested_hardware: cpu-basic
pinned: false
license: gpl-3.0
---

# XRAG

XRAG 的 M1 服务：把目录里的 Markdown/TXT 按文件版发布，再做关键词检索和固定引用读取。图、Wiki、三种 Client 和 Recipe 不在这个部署里。

## Space

- GitHub：<https://github.com/BlueSkyXN/XRAG>
- Hugging Face Space：<https://huggingface.co/spaces/BlueSkyXN/XRAG>
- 应用：<https://blueskyxn-xrag.hf.space>
- 形态：Docker、Protected、`cpu-basic`
- 端口：`7860`
- 持久化：私有 Bucket `BlueSkyXN/xrag-data` 读写挂载到 `/data`

浏览器和 API 都经过 Nginx Basic Auth。`/healthz` 只用于平台存活检查，不读取数据库。

```text
Variable: ADMIN_USERNAME
Secret:   ADMIN_PASSWORD
```

`ADMIN_PASSWORD` 至少 16 个字符。明文只在启动时生成 `/tmp` 里的口令摘要，不写入镜像、Git 或 Bucket。本机的部署值放在被 Git 忽略的 `local/hfs-targets/production.env`。

```text
standard:       HFS v3.0
project_class:  preview
target_role:    primary
sovereignty:    port
lane:           product
version_source: commit
visibility:     protected Space / private bucket
```

首次挂载 Bucket 后写入目录种子：

```bash
./scripts/prepare-bucket-prefix.sh --apply
```

## 本地

```bash
go test ./...
bash scripts/static-check.sh
go run ./cmd/xrag-server init -data data
go run ./cmd/xrag-server kb add -data data -id demo -name Demo
go run ./cmd/xrag-server source add -data data -kb demo -id docs -path ./corpus
go run ./cmd/xrag-server build -data data -kb demo
go run ./cmd/xrag-server keyword -data data -kb demo -q E203
```

`go run ./cmd/xrag-server http -data data` 在 `127.0.0.1:8080` 提供同一套 API。Space 里这个端口只给本机 Nginx 使用。

查询只读已发布文件版。来源改动在构建完成前不会进入检索，旧 `xr1:` 引用仍读原来的内容和行号。

## License

GPL-3.0。
