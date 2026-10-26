# Live 发布入口

`ops/deployment/live/live-release.py` 是 live 环境的统一发布计划入口，`ops/deployment/live/live-release.ps1` 是 Windows 调用包装器。默认操作是只读计划，不会构建镜像、连接 ECS、修改数据库或重启容器。

## 生成计划

在仓库根目录执行：

```powershell
.\ops\deployment\live\live-release.ps1 -Environment live -Commit HEAD -Plan
```

计划会检查 Git revision、工作区漂移、主站/供应商 Compose 和 Dockerfile、Caddy 配置，并在仓库外生成 `release-manifest.json` 和 `PLAN.txt`。默认发布组件是 `main,supplier`，也可以使用 `-Components supplier`。

## 执行边界

未提交的发布工具和 Dockerfile 可以通过 `--include-release-tooling` 显式纳入；只接受限定路径，逐文件记录并重新校验 SHA-256，其他业务工作区改动仍会阻止发布。不需要为发布自动 commit。源码、镜像和受限补丁均归档到发布目录。

```powershell
python .\ops\deployment\live\live-release.py --include-release-tooling --execute
```

无迁移版本默认只验证 schema（主库296 / 模块13）。当前 `--allow-migrations` 没有声明式迁移清单时会在激活前拒绝执行，不能把任意新增 SQL 隐式带上线。

执行入口必须显式传入 `-Execute`。执行顺序为：干净源码归档、后端检查、双镜像构建、镜像校验、上传 ECS、数据库和应用数据备份、隔离 PostgreSQL 恢复、声明式迁移（只有 `-AllowMigrations` 才启用）、Compose 激活、容器和公网验收、发布记录。任一激活阶段失败时，服务器端只恢复发布前环境和镜像，不自动降级数据库。

需要人工回退时使用同一个发布 ID：

```powershell
python .\ops\deployment\live\live-release.py -Rollback live-20261026-120000-<commit>
```

脚本不会执行 `docker compose down -v`，不会打印生产环境变量，也不会把环境文件或数据库 dump 下载回本地。

每个 manifest 固定记录目标 `root@47.236.9.72`、Compose 项目 `sub2api`、源 commit、部署文件 hash、预期主库 296 / 供应商库 013 版本和不允许执行的破坏性命令。服务器端执行器位于同一发布目录，按 manifest 中声明的目标和组件运行。

## 设计约束

- 构建使用干净的 `git archive` 上下文，发布产物和验证记录放在仓库外。
- 主站使用 `ops/deployment/compose.production.yml`，供应商使用其生产 overlay。
- 迁移必须是声明式、可验证的阶段；失败时禁止激活新镜像。
- 服务回滚不自动回退数据库；包含不可逆迁移的版本必须提供前向兼容策略。
- 发布记录不包含生产密钥、token 或完整环境文件内容。
