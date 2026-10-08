# 发版说明（维护人视角）

发版 = **打一个 tag 推上去**，剩下的交给 [`.github/workflows/release.yml`](../.github/workflows/release.yml)。

## 一、发版流程

```bash
# 1) 确认 main 干净且 CI 全绿（.github/workflows/ci.yml：gofmt / go vet / go test / 前端构建与测试）
git switch main && git pull
git status

# 2) 本地演练（可选但推荐）：只构建不发布
#    Actions → Release → Run workflow → dry_run = true
#    它会把三平台包与镜像都构建一遍、并真跑容器冒烟，但不产出 Release、不推镜像。

# 3) 打 tag 并推送（注释 tag，写明本版要点）
git tag -a v1.0.0 -m "Lyidc_OEM v1.0.0"
git push origin v1.0.0
```

推送后流水线自动完成：

| job | 产物 |
| --- | --- |
| `frontend` | 前端构建一次，作为 artifact 供打包复用（三平台**不会**各跑一次 `npm ci`） |
| `build-packages` | 三平台发布包（linux/amd64、linux/arm64、windows/amd64）+ `.sha256` 摘要 |
| `release` | GitHub Release：附全部产物，更新说明由 `generate_release_notes` 按 PR/提交自动生成 |
| `docker` | GHCR 双镜像（`lyidc-oem-backend` / `lyidc-oem-frontend`），多架构 `linux/amd64,linux/arm64` |
| `smoke` | 真起 MySQL + 两个容器：跑完安装向导全流程，断言健康检查、反代与就绪态 |

## 二、tag 命名规则

- 正式版：`v<主>.<次>.<修订>`，例如 `v1.0.0`、`v1.2.3`（**必须** `v` 前缀，`v*` 才会触发流水线）。
- 预发布：`v1.1.0-rc.1`、`v1.1.0-beta.2` —— 带 `-` 的 tag 会被标成 Release 的 **Prerelease**。
- 镜像 tag 由 `docker/metadata-action` 从 git tag 推导：

| 触发 | 后端/前端镜像 tag |
| --- | --- |
| 推送 `v1.2.3` | `v1.2.3`、`1.2`、`sha-xxxxxxx`、`latest` |
| 手动 run（dry_run=false，分支） | 仅 `sha-xxxxxxx` |

> `latest` 由 `type=raw,value=latest,enable=<ref 是 v* tag>` 显式给出——分支构建**不会**动 `latest`，
> 避免把正式版覆盖成开发提交。

## 三、手动演练（workflow_dispatch）

Actions → Release → Run workflow，`dry_run` 默认 **true**：

- `true`：构建三平台包与两个镜像（多架构编译，不推送）、跑容器冒烟；跳过 Release。用于验证 Dockerfile/workflow 改动。
- `false`：等同于一次正式发布，但**仅当 ref 是 `v*` tag 时**才会创建 Release；在分支上跑只会推 `sha-*` 镜像。

## 四、发布后核对清单

1. `gh run watch`（或 Actions 页面）确认 5 个 job 全绿。
2. Release 页面：三个平台的 `tar.gz`/`zip` 与 `.sha256` 齐全，更新说明已自动生成。
3. GHCR 两个包存在，且 tag 与预期一致（`latest`、`vX.Y.Z`、`X.Y`、`sha-xxxxxxx`）。
4. 新增版本若有**新迁移**，在 docs/deploy.md「升级」里确认升级步骤已写明（`lyidc-migrate up`）。
5. 匿名可拉取性（安装部署的默认前提，只需在首次发版或改包可见性后检查）：

```bash
OWNER=langfordkuo          # 全小写
for img in lyidc-oem-backend lyidc-oem-frontend; do
  TOKEN=$(curl -s "https://ghcr.io/token?scope=repository:${OWNER}/${img}:pull" | jq -r .token)
  curl -s -o /dev/null -w "${img} manifest: %{http_code}\n" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Accept: application/vnd.oci.image.index.v1+json" \
    "https://ghcr.io/v2/${OWNER}/${img}/manifests/latest"
done
# 期望两个都是 200；403/404 说明包是私有的：
# GitHub → 该包 → Package settings → Change visibility → Public
```

## 五、本地等价命令

CI 做的事本地都能复现（无 Docker 时跳过镜像相关步骤）：

```bash
bash scripts/package.sh -v v1.0.0                # 三平台打包（需先 cd frontend && npm run build）
python scripts/check_release_workflow.py         # 交付资产自检（workflow/Dockerfile/compose 结构+语法）
cd backend && go test ./...                      # 后端测试
cd frontend && npm test && npm run build         # 前端测试与构建
```
