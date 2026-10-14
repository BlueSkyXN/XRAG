# 临时（staging）Space 验证流程

> **2026-10-14 决定：本次切换跳过 staging，直接部署正式 Space。**现有线上数据无保留价值（用户确认），staging 保护的两个对象（生产数据、可用性）都不存在。保留本文流程供以后使用——等线上开始积累真实数据、停机有代价时，重大变更（换基础镜像、换入口机制）仍应先过一遍 staging。

原定位：正式 Space 切到镜像部署前的验证环境。目标：与生产同构（镜像、端口、入口、平台检查），数据零风险。

## 数据卷决策（2026-10-14）

不新建 Bucket。临时 Space **只读**挂载现有 `BlueSkyXN/xrag-data`，服务数据目录指向容器本地 `/tmp/xrag-staging`：

- 生产 Space 正在以读写模式使用 `xrag-data`（SQLite 在 `/data/xrag`）。HF 卷挂载只支持整桶，不支持子路径；若临时 Space 也读写挂载，两个进程会同时写同一个 SQLite，数据损坏风险不可接受。
- 只读挂载满足验证需要：登录页、检索、`/healthz` 的 commit、cookie `Secure` 全部不写库；上传/构建等写操作的验证改在 `/tmp` 数据目录上做，行为与生产同构。
- 验证完成后删除临时 Space 即可，桶里不留任何 staging 产物。

## 建立临时 Space（手动，一次性）

```bash
# 1. 建桶里已有的公开存活检查不需要；建 protected Space（cpu-basic, docker）
#    README 直接用 deploy/hf/README.md 渲染产物（front matter 不变）
hf spaces create XRAG-staging --space-hardware cpu-basic --space-sdk docker

# 2. 只读挂载生产桶（注意 :ro；绝不能省略）
hf spaces volumes set BlueSkyXN/XRAG-staging \
  -v "hf://buckets/BlueSkyXN/xrag-data:/data:ro"

# 3. secrets/variables（值取自本机 local/hfs-targets/，不写入仓库）
hf spaces secrets set BlueSkyXN/XRAG-staging SECRET ADMIN_PASSWORD
hf spaces variables set BlueSkyXN/XRAG-staging VARIABLE ADMIN_USERNAME xrag
# 数据目录指向容器本地（可写），绕开只读的 /data
hf spaces variables set BlueSkyXN/XRAG-staging VARIABLE XRAG_DATA_DIR /tmp/xrag-staging
```

注意：`XRAG_DATA_DIR=/tmp/xrag-staging` 与默认 `/data/xrag` 不同，唯一目的是配合只读挂载。生产部署**不带**这个变量。

## 验证清单（deploy-hf 部署正式 Space 同一个 digest 后）

1. 登录页出现，用户名密码可登录。
2. `/healthz` 的 `commit` 等于镜像的 revision。
3. 会话 cookie 带 `Secure`（HF 全程 HTTPS + `XRAG_TRUST_FORWARDED_PROTO=1`）。
4. 检索返回结果（读的是生产数据的副本视图，只读）。
5. 上传一个小文件并触发增量构建（写入 `/tmp` 数据目录），重启 Space 后确认：`/tmp` 数据不保留属预期，只读桶内无新文件。
6. HF 入口的三项平台检查在真实环境成立（UID 1000、`/data` 是挂载点、PORT）。

## 清理

```bash
hf spaces volumes delete BlueSkyXN/XRAG-staging
hf spaces delete BlueSkyXN/XRAG-staging
```

桶不动、正式 Space 不动。删除前无需备份：staging 从未写桶。

## GHCR 镜像公开性（部署前置）

临时与正式 Space 拉取 GHCR 镜像都不带凭据，镜像包必须为 public。在 L2 合入 `main`（首次推送自动发生）后、第一次运行 deploy-hf 前设置一次：

GitHub → Package `blueskyxn/xrag` → Package settings → Change visibility → Public（会收到确认邮件，需点过生效）。

用 Packages API 验证（本机 gh 需带 `read:packages`）：

```bash
gh api /user/packages/container/xrag --jq .visibility   # 期望输出 public
```

注意：**不要**用 ghcr.io 的匿名 token 端点当探测——实测它对公开包也返回 DENIED（匿名 token 不发给未认证客户端），403 无法区分"包不存在/私有/公开"。

deploy-hf workflow 会在每次部署前自动检查包的 `visibility` 字段，非 `public` 即中止（见 `.github/workflows/deploy-hf.yml`）。
