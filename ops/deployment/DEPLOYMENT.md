# 私有启动与发布

**当前发布（2026-10-23）**：主站和供应商服务已升级至 `3b9cbbd6d0b26ed7926842e69de935f93aa271ee` 加发布配置补丁。主站管理页面和供应商同源API已验收；供应商域名接入由用户暂缓。当前组合命令、数据库角色、备份及回退见 [供应商生产部署](../../extensions/supplier/deploy/README.md)。下方为2026-10-21首次部署记录，旧版本号和“不修改供应商”的范围仅适用于首次部署。

执行范围：本地构建 fork 固定提交的 linux/amd64 镜像，save/SCP/load 发布到已购买的 ECS，连接托管 PostgreSQL 和 Redis，初始化并验证管理员登录。用户随后授权配置 ECS 的 Caddy HTTP 入口，沿用 Cloudflare Flexible；不修改供应商模块、上游业务逻辑、SSH、数据库权限或域名解析。管理员合规承诺必须由实际管理员阅读并确认，不能由部署脚本自动接受。

## 已确认部署参数

- 源码提交：`b12696932788a41cfab7f8c25cfa807c1a7bc238`，版本 `0.2.17`。
- ECS：新加坡，4 vCPU / 8 GiB、40 GiB 磁盘、5 Mbps；Docker 已安装。
- 托管 PostgreSQL：18，2 vCPU / 4 GiB、50 GiB；数据库 `sub2api`，SSL 关闭。
- 托管 Redis：7、1 GiB、标准主从，DB 0，TLS 关闭。
- PG 白名单按用户决定保留 `172.16.0.0/12`；Redis 放行应用 ECS 私网单 IP。
- 实际连接地址、账号及密码只写入受限运行配置，不写入版本库。
- 以上实际采购及连接状态取代早期 PLAN.md 中未购买、100 Mbps、100 GiB 和 Redis 2 GiB 的预估。网站采用 Cloudflare Flexible：浏览器至 Cloudflare 使用 HTTPS，Cloudflare 至 ECS 使用未加密 HTTP。用户知悉风险并选择暂时保留，安全组白名单不能替代链路加密。

## 镜像交付

在不含本机秘密文件的固定提交导出目录执行构建，镜像标签显式指定源码 SHA。

```sh
docker buildx build --platform linux/amd64 --load --tag sub2api-local:b12696932788-local --build-arg COMMIT=b12696932788a41cfab7f8c25cfa807c1a7bc238 --build-arg VERSION=0.2.17 .
docker save -o sub2api-b12696932788-local.tar sub2api-local:b12696932788-local
scp sub2api-b12696932788-local.tar root@ECS:/opt/sub2api/releases/
```

上传前后比较镜像包 SHA256；服务端 `docker load -i` 后检查 OS、架构、RootFS 层、运行配置和创建时间与本机一致。本机 classic 存储显示 config ID，服务端 containerd 显示 OCI manifest descriptor ID，两者字符串可能不同，不能只凭显示 ID 判定包变更。不使用此前在 ECS 直接构建的同提交镜像启动。

## 私有启动

把 `compose.production.yml` 和真实 `production.env` 放入 `/opt/sub2api`，目录 0700、环境文件 0600。参考 `production.env.example`，固定保存 JWT/TOTP 密钥，显式指定管理员密码，避免由程序生成并写入日志。

```sh
cd /opt/sub2api
docker compose -p sub2api --env-file production.env -f compose.production.yml config --quiet
docker compose -p sub2api --env-file production.env -f compose.production.yml up -d
docker compose -p sub2api --env-file production.env -f compose.production.yml ps
curl --fail http://127.0.0.1:8080/health
```

该文件仅启动应用，不启动数据库或 Caddy；Caddy 单独作为宿主机 systemd 服务运行。应用端口硬绑定 `127.0.0.1:8080`。固定项目名 `sub2api`，保留 `sub2api_sub2api_data` 命名卷。不执行 `down -v`。应用日志轮转为 10 MiB × 3。

访问后台使用本机 SSH 隧道，不开放 ECS 8080：

```sh
ssh -N -L 127.0.0.1:18080:127.0.0.1:8080 root@ECS
```

浏览器访问 `http://127.0.0.1:18080`。健康检查只证明 HTTP 进程就绪，还需验证登录、profile、管理员角色及管理员合规状态。

## Caddy 入口与验证

- Caddy 使用官方稳定版 APT 软件源，本次安装版本为 2.11.7；安装时先屏蔽服务自动启动，避免公开默认站点。
- 仓库 `Caddyfile` 部署到 `/etc/caddy/Caddyfile`，监听 80，反代 `127.0.0.1:8080`。仅接受 `gomoai.ai`、`www.gomoai.ai`；未知 Host 返回 404，非 Cloudflare 来源返回 403。本机回环允许运维验证，但不信任其伪造的转发头。
- Cloudflare IPv4 网段于 2026-10-21 从官方列表核对；安全组与 Caddy 双重限制。域名当前仅配置 IPv4 源站；若启用 IPv6 源站，需同步审查两层规则。
- Caddy 仅从可信 Cloudflare 来源解析 `CF-Connecting-IP`，严格解析并覆盖传给应用的 XFF、X-Real-IP、CF-Connecting-IP、转发协议和 Host；删除其他歧义身份头。不启用源站 HTTPS 跳转，避免 Flexible 重定向循环。
- 原生 Caddy 访问 Docker 发布端口时，应用实际观察到宿主机桥接网关 `172.18.0.1`。运行环境 `SERVER_TRUSTED_PROXIES` 设为 `127.0.0.1/32,172.18.0.1/32`，不信任整个 Docker 子网。重建 Docker 网络时必须重新确认地址，不能盲目沿用。
- Caddy 管理接口仅监听本机 2019；应用 8080 不对公网开放。不启用额外响应压缩或全局流式强制刷新；SSE 按 Caddy 原生机制处理。

```sh
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
systemctl reload caddy
curl --fail -H 'Host: gomoai.ai' http://127.0.0.1/health
```

已通过：本机经 Caddy 的健康检查、前端、管理员登录、权限和合规状态；应用镜像、卷、数据库表和迁移数不变；伪造转发头被清洗。隔离模拟测试覆盖可信/不可信代理、非法 Host、拒绝来源和 SSE 首段及时返回。隔离 SSE 不是实际模型或 Cloudflare 链路验收。

## 域名切换前后剩余条件

1. 实际管理员已自行确认 `docs/legal/admin-compliance.zh.md`，接口验证 required=false。
2. `api_key_acl_trust_forwarded_ip` 实际读取为 false，重建后复核通过；没有为此执行设置写入。不能仅凭环境变量判断持久化设置。
3. 用户已在安全组将 80 收紧为 15 个 Cloudflare IPv4 网段（截图核对）；Caddy 已部署。SSH 22 仍允许全网，本次不修改，后续单独收紧至运维公网来源。
4. GoDaddy 名称服务器尚待切换；切换前核对 DNS 记录完整性及 DNSSEC，之后确认 Cloudflare 激活、边缘证书可用、Always Use HTTPS 开启，API 不被缓存或交互式挑战拦截。必须再验证真实 Cloudflare 客户端 IP 与端到端 HTTPS/流式调用；接入上游账号后才能验证真实模型调用。
5. 高权限 PG 账号目前为用户指定的初始化连接账号；公网运营前评估迁移到权限覆盖应用 schema DDL 的专用账号，不能直接撤销当前对象所有权而不验证迁移。

## 备份与恢复边界

首次启动前已完成空库检查、事务内 DDL/DML 探针并回滚，预初始化备份保存在 ECS `/opt/sub2api/backups/pre-first-start-b12696932788.dump`。

启动失败可停止应用以停止继续写入，保留日志、运行配置、数据卷和数据库。不要自动删除表、清空 Redis 或恢复备份。源码中的前向迁移可能改变数据库结构，停止容器不等于数据库回滚；恢复数据库需先核对备份、影响范围与业务数据。

配置回滚脚本只还原指定的配置副本，不会变更云数据库或撤销迁移。首次上线没有旧业务实例可切回。

Caddy 启用前的环境与配置备份位于 ECS `/opt/sub2api/backups/ingress-20261021/`（仅 root 可读）。其中 `ROLLBACK-ingress.sh` 停止并禁用 Caddy、还原先前运行环境并重建同镜像应用，恢复私有入口；不回滚数据库、DNS 或云安全组。只在确认需要退回私有状态时执行。文件副本回滚验证不等于已在生产执行整体回滚。
