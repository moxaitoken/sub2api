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
- 仓库 `Caddyfile` 部署到 `/etc/caddy/Caddyfile`，监听 80，反代 `127.0.0.1:8080`。仅接受 `gomoai.ai`、`www.gomoai.ai`；未知 Host 返回 404，非 Cloudflare 来源返回 403，但下文记录的 ALB 健康检查例外除外。本机回环允许运维验证，但不信任其伪造的转发头。
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


## home-20261023-171656

用户授权将最新提交 `cf569d288156af7e4bf891d544ef7d5ec16ae21c` 部署至现有ECS。对比上一线上源码仅首页默认页脚移除GitHub链接，文档与版权保留。只更新主站镜像 `sub2api-local:home-20261023-171656`；供应商容器ID、镜像及Caddy路由保持不变，env仅SUB2API_IMAGE变更，无迁移/依赖/权限调整。主站构建含i18n/类型检查，首页聚焦测试及同输入BASELINE→MODIFIED→ROLLBACK副本测试通过（旧版/回滚版GitHub缺失断言预期失败，新版通过，回滚源hash一致）。

备份及主站镜像回退：`/opt/sub2api/releases/home-20261023-171656/before`、`/opt/sub2api/releases/home-20261023-171656/ROLLBACK.sh`。两库备份在无网络临时PG中实际恢复校验；回退脚本仅syntax及env副本实际测试，未为测试回退生产。线上健康、主站管理员登录及供应商管理读取、未登录401、首页静态chunk通过；浏览器验收未执行。供应商服务未重启，数据库记录及现有密钥不变。


## policy-20261023-181228

用户授权发布已核对工作区快照（基线 `a4195324e70bc08c8cc51b9f62632c4b149b6261` 加manifest精确记录的定制升级策略代码）。仅主站镜像更新为 `sub2api-local:policy-20261023-181228`，供应商容器ID、Caddy、应用环境与数据库schema保持不变。两个原地二进制替换接口返回403/IN_PLACE_UPDATE_DISABLED，版本弹层保留只读提示/刷新；独立Go路由stub和Vue测试通过。先比对部署二进制SHA与已测镜像，再验证新版本拒绝策略，未在旧生产版本调用更新写接口。

备份和主站镜像回退：`/opt/sub2api/releases/policy-20261023-181228/before`、`/opt/sub2api/releases/policy-20261023-181228/ROLLBACK.sh`。两库备份已在隔离PG实际恢复；回滚只对副本实测，未为验证回退生产。旧镜像会重新开放旧的原地升级能力，紧急回退后不得使用该旧功能。公网主站/供应商健康及静态资源通过，无迁移或依赖升级。


## group-20261023-202917

用户授权部署当前代码，固定源码 `ac43ab2b591fe88be6db666807752b5c21bd8ace`，完整字节见发布source-manifest。主站 `sub2api-local:group-20261023-202917` 先发布并验证受保护能力查询 `schedulable_on_create=true`，供应商 `sub2api-supplier:group-20261023-202917` 后发布。供应商库新增011分组接入配置/初始化标记/审计表并补运行角色权限；DDL及审计UPDATE/DELETE被拒绝，主站296迁移不变。分组默认关闭（enabled数量0），初始化完成；不自动开放生产分组、不创建真实上游账号/调用/财务写入，保留旧隔离组。新代码不再依赖隔离组先创建，改为直接目标组创建时暂停，归属/配置校验后启用。旧调用省略schedulable仍默认true。

两库及数据卷备份：`/opt/sub2api/releases/group-20261023-202917/before`；两库在隔离PG实际恢复核对。源码同输入BASELINE暂停断言失败exit1→MODIFIED通过exit0→ROLLBACK恢复失败exit1，恢复hash一致，patch重建一致。主站/供应商Go、前端及隔离PG测试通过，公网健康/资源与管理员只读API、未登录401通过；浏览器及真实Claude验收未执行。Caddy、域名、密钥、供应商env未变。

**011恢复边界**：旧供应商程序会拒绝未知011，不能只回退旧镜像。服务端 `/opt/sub2api/releases/group-20261023-202917/ROLLBACK.sh` 仅实测还原指定env副本，无参数拒绝exit3以避免错误生产回退。生产恢复需011兼容修复版本，或另行授权维护窗口恢复匹配备份；未执行生产回退，不删除迁移历史。


## copy-20261023-210751

用户授权部署最新提交 `c262db5e4f57604b4e7804fad073d64532b68c24`（source-manifest记录固定源码），仅更新供应商门户镜像 `sub2api-supplier:copy-20261023-210751`。AccountOnboardingView展示完整只读授权链接，复制当前URL、成功提示，剪贴板缺失/拒绝时提供手动复制，不新增授权会话。主站容器ID、供应商后端二进制SHA、Caddy、供应商env、011库结构与现有分组不变；无迁移/依赖升级/真实OAuth或模型调用。

完整门户测试/类型/构建通过；3个同输入复制fixture BASELINE失败exit1→MODIFIED通过exit0→ROLLBACK恢复失败exit1，源hash一致、补丁重建一致。两库dump已在无网络临时PG实际恢复。公网HTML/资源与镜像字节一致、健康200、未登录401、www308通过；未执行浏览器渲染验收。

备份和回退：`/opt/sub2api/releases/copy-20261023-210751/before`、`/opt/sub2api/releases/copy-20261023-210751/ROLLBACK.sh`。本次旧供应商镜像为兼容011的group版本，脚本可仅回退供应商镜像env和容器；带一个参数还原env副本已实测，未为验证执行生产回退。禁止使用更早的pre011供应商程序。

## mapping-20261024-000000

用户授权将当前提交 6ae3edbcb4d605ef221d7091f0876a595bb53e69 部署到现有 ECS。主站镜像 sub2api-local:mapping-20261024-000000 先切换，供应商镜像 sub2api-supplier:mapping-20261024-000000 后切换；Caddy、域名、密钥、供应商环境文件保持不变。供应商数据库从011前进迁移至012，增加供应商分组名映射、多目标OAuth分组绑定及历史回填；旧的 group_access(1) enabled/version1 状态保留，不新增启用分组。

发布前两库及应用数据备份位于 /opt/sub2api/releases/mapping-20261024-000000/before，在隔离 PostgreSQL 实际恢复校验。主站296迁移、4用户、2账号不变；供应商迁移数12、012列与运行角色权限校验通过。主站与供应商容器健康、重启数0、端口仅回环；gomoai.ai/goccmax.com健康和HTML/静态资源200，管理员登录/能力接口200，供应商未登录接口401，分组映射读取200。

主站构建、聚焦后端/前端测试、供应商unit/vet/build和门户104项测试通过。供应商全量Postgres集成套件退出1：两条断言仍写死迁移数11，另有用量回填重试时序失败；该证据已记录，未静默跳过。真实Claude授权、调用和非零消费仍待验收。

回退边界：/opt/sub2api/releases/mapping-20261024-000000/ROLLBACK.sh 仅允许还原指定env副本；无参数在012已应用后退出3并拒绝启动旧供应商二进制。生产数据库不自动回退，必须使用012兼容修复镜像或另行授权的匹配备份恢复。角色记录位于发布目录 role-records/。


## settings-20261024-152100

用户授权发布固定提交 1d02333df1913dca62152adad0a643752c090d4d。主站 sub2api-local:settings-20261024-152100 和供应商 sub2api-supplier:settings-20261024-152100 已更新。013仅新增OAuth account_settings参数快照，旧未完成授权保留旧默认；已有账号和分组未批量修改。供应商全量后端248通过事件/无跳过、门户110项测试/类型/构建、主站18项聚焦测试及嵌入构建通过。

两库及卷备份 /opt/sub2api/releases/settings-20261024-152100/before 已实际隔离恢复；模块备份在隔离PG演练012→013通过，分组及会话行数保持。生产013迁移、默认/列权限检查通过；两容器健康/重启0/仅回环端口，公网HTTPS健康/HTML/初始资源200、管理员登录及读取200、未认证401。Caddy/密钥/供应商env不变。真实Claude调用和非零消费未测试。

恢复边界：/opt/sub2api/releases/settings-20261024-152100/ROLLBACK.sh 还原指定env副本已实测exit0，无参数拒绝exit3，不自动回退数据库或启动pre013程序；采用013兼容前向修复或另行授权的匹配备份维护恢复。源码同输入BASELINE失败exit1→MODIFIED通过exit0→ROLLBACK恢复失败exit1，补丁精确重建、恢复hash一致。角色文件归档于role-records。


## metrics-20261024-162900

用户授权发布固定提交 c50627547d2bf4aefc24b417de2e219eb66c479d。主站 sub2api-local:metrics-20261024-162900 和供应商 sub2api-supplier:metrics-20261024-162900 已更新。账号池以可选include_metrics=1只读投影并发上限/调度优先级及已同步今日/累计官方消耗；保留精确金额字符串、null不可用及partial提示，不冒充实时指标。未请求参数保持旧响应。无迁移、依赖或权限变更，供应商schema013和主站296迁移不变。

两库及卷备份 /opt/sub2api/releases/metrics-20261024-162900/before 已实际隔离恢复。供应商后端完整检查和前端测试/类型/构建、主站嵌入构建通过。主站原测试1项仍期待旧URL，保留失败，仅在仓库外验证副本补include_metrics=1预期后，聚焦及管理员供应商测试通过；未修改运行时代码或声称原套件全通过。公网HTTPS健康/页面/初始资源200、管理员登录和指标读取200、未认证401、兼容旧读取不带metrics。已有账号/用户/分组保持、Caddy和供应商env不变。真实Claude模型和非零消费验收未执行。

源码同输入BASELINE失败exit1→MODIFIED通过exit0→ROLLBACK恢复失败exit1，补丁精确重建、恢复hash一致。/opt/sub2api/releases/metrics-20261024-162900/ROLLBACK.sh 可回退两镜像到本次前schema013兼容settings版本；仅语法和env副本恢复已实测，未为验证回退生产，不回退数据库。角色记录位于role-records。


## sync-20261024-174700

用户授权发布 8e90acff70e55a362e72d0bbf4de048fa345f909，两镜像 sub2api-local:sync-20261024-174700 / sub2api-supplier:sync-20261024-174700。刷新include_metrics=1时在同次主站读取中同步状态：active→approved，schedulable→running/paused；失败/pending有标记，归属/版本/进行中操作保护，变化事务写实际阅读者审计；不更新主站状态/分组/账务，不重放OAuth。无迁移/依赖/权限调整，供应商013、主站296迁移不变。

两库及卷备份 /opt/sub2api/releases/sync-20261024-174700/before 已实际隔离恢复；供应商完整后端/门户测试、类型与两套嵌入构建通过。主站一条原URL断言仍未补include_metrics=1，原失败保留；仅仓库外验证副本更新预期后相关测试通过，运行源码未改。线上验证状态同步DTO与实际落库/主站状态一致，重复刷新不增加版本/审计，历史操作不变，主站账号与分组不变；公网HTTPS健康/页面/资源200、管理员读取200、未认证401。真实模型调用未执行。

源码同输入BASELINE失败exit1→MODIFIED通过exit0→ROLLBACK恢复失败exit1，补丁精确重建、恢复hash一致。/opt/sub2api/releases/sync-20261024-174700/ROLLBACK.sh 支持回退到此前013兼容metrics镜像，语法/env副本恢复已测，未为测试回退生产；代码回退不撤销已镜像的主站观察值/审计，不回退数据库。四角色及补充记录保存在role-records。


## pool-20261026-100500

用户授权发布 9e1755a240456b142c1f64152d8ea1d9f47634a8，两镜像 sub2api-local:pool-20261026-100500 / sub2api-supplier:pool-20261026-100500。刷新include_metrics=1时在同次主站读取中同步状态：active→approved，schedulable→running/paused；失败/pending有标记，归属/版本/进行中操作保护，变化事务写实际阅读者审计；不更新主站状态/分组/账务，不重放OAuth。无迁移/依赖/权限调整，供应商013、主站296迁移不变。

两库及卷备份 /opt/sub2api/releases/pool-release-20261026-100500/before 已实际隔离恢复；供应商完整后端/门户测试、类型与两套嵌入构建通过。主站一条原URL断言仍未补include_metrics=1，原失败保留；仅仓库外验证副本更新预期后相关测试通过，运行源码未改。线上验证状态同步DTO与实际落库/主站状态一致，重复刷新不增加版本/审计，历史操作不变，主站账号与分组不变；公网HTTPS健康/页面/资源200、管理员读取200、未认证401。真实模型调用未执行。

源码同输入BASELINE失败exit1→MODIFIED通过exit0→ROLLBACK恢复失败exit1，补丁精确重建、恢复hash一致。/opt/sub2api/releases/pool-release-20261026-100500/ROLLBACK.sh 支持回退到此前013兼容metrics镜像，语法/env副本恢复已测，未为测试回退生产；代码回退不撤销已镜像的主站观察值/审计，不回退数据库。四角色及补充记录保存在role-records。


## live-20261026-100501-1ad1889b5159

使用统一 live-release.py --include-release-tooling --execute 发布固定提交 `1ad1889b5159bcb3075e7c96589fc0f456834b19` 和hash记录的发布工具/Dockerfile补丁；未commit/push。主站 `sub2api-main:live-20261026-100501-1ad1889b5159`，供应商 `sub2api-supplier:live-20261026-100501-1ad1889b5159`。供应商新登录固定7天，不因活动续期；管理员与OAuth期限独立，不重写既有会话到期值。

供应商后端276通过事件、0跳过，门户121测试及类型/构建通过，主站i18n/类型/嵌入构建通过；未声称重跑主站完整单元套件。两库dump在无网络临时PG实际恢复，schema主库296/供应商13不变；Caddy、供应商环境配置hash保持。两服务健康、0重启、仅回环端口；公网HTTPS健康/页面200、管理员登录及4个供应商概览200、未认证401、初始静态资源200。未触发真实模型/OAuth/付款。

服务器归档 `/opt/sub2api/releases/live-20261026-100501-1ad1889b5159`，备份 `before/`，回退入口 `ROLLBACK.sh`。可恢复两镜像/production.env，schema漂移时拒绝自动镜像回退；无数据库降级。环境副本实际恢复hash一致，未为测试回退生产。四角色及执行器同输入回归记录：`C:/Users/tirol/.codex/artifacts/sub2api-deploy/20261026-live-script`；旧执行器失败→修正通过→副本回滚恢复旧行为。


## live-20261027-094251-b373e7219789

用户修正 Boolean 默认值后，使用原版 `ops/deployment/live/live-release.ps1 -Environment live -Commit b373e7219789620ab815cab61ea31ce8734b5ba4 -Execute` 成功发布主站/供应商。脚本未修改、工作区干净、零源码overlay、未skip测试；仅进程内使用现有系统代理。管理员详情隐藏添加账号，供应商门户默认保留；未改变后端/schema/权限。

供应商后端276通过事件/0跳过，门户124测试及类型/构建通过；主站i18n/类型/嵌入构建及管理员详情聚焦测试通过，未重跑主站完整后端套件。UI同输入旧错误默认值BASELINE失败exit1→修正MODIFIED通过exit0→源副本ROLLBACK失败exit1，恢复hash一致；记录在仓库外发布目录ui-verification。

两库dump在无网络临时PG实际恢复，主296/供应商13不变。两镜像 revision精确匹配commit，healthy、0重启、仅回环端口；公网健康/页面200、8项静态资源与源站hash一致，管理员登录及4个供应商概览200、未认证401。Caddy和供应商环境hash不变，未触发真实模型/OAuth/付款。

备份 `/opt/sub2api/releases/live-20261027-094251-b373e7219789/before`，回退入口同目录`ROLLBACK.sh`；仅环境副本实际执行恢复exit0/hash一致，未为测试回退生产、无DB降级，schema漂移拒绝镜像回退。四角色延伸记录在`C:/Users/tirol/.codex/artifacts/sub2api-deploy/20261026-live-script`。此前两次失败尝试保留，不覆盖旧记录。


## api-direct-20261028-080727

用户授权新增 `api.gomoai.ai` 的独立模型 API HTTPS 入口，不修改 Sub2API 核心代码、镜像、数据库或现有网站/门户路由。用户已新增 A 记录至现有 ECS，设置 DNS only，并在安全组新增 TCP 443 的 IPv4 全网访问；HTTP 80 仍限制 Cloudflare 网段，应用 8080/8091 保持回环监听。

Caddy 仅为 API 子域名开启公开证书自动管理，固定 ACME TLS-ALPN（443）验证，不要求放开 80。`auto_https disable_redirects` 保持原网站 Flexible HTTP 回源不产生 HTTPS 重定向；严格 SNI/Host 检查避免混用主机名。新增入口仅匹配配置中的模型 API 路径；根页、管理员、登录及供应商管理接口拒绝访问。直连请求按 TCP peer 重建 XFF、X-Real-IP、CF-Connecting-IP，删除 Forwarded/True-Client-IP，不信任客户端提供的转发身份。

灰云入口公开源站 IP，不享有 Cloudflare 代理的 WAF/隐藏源站能力；共享 ECS 的网络层攻击仍可能影响主站。现有网站的 Flexible 回源加密问题未在本次处理。原域名的 API 入口仍保留，客户端只有切换到新 API 地址才绕开 Cloudflare 的读取超时；上游模型慢及客户端自身超时不会因此自动解决。

修改前配置与回退脚本：`/opt/sub2api/releases/api-direct-20261028-080727/BASELINE.Caddyfile`、同目录 `ROLLBACK.sh`。回退脚本接受目标配置文件，仅恢复配置字节；生产回退须先确认现场无后续变更，再运行脚本、`caddy validate`，最后 `systemctl reload caddy`。回滚副本已验证，不为测试回退生产；证书自动续期由 Caddy 负责，私钥不进入仓库或聊天。

四角色及验收记录：`C:/Users/tirol/.codex/artifacts/sub2api-api-direct-20261028-080727/`。模型接口未认证返回 401，后台/管理路径返回 404；主站及门户公网页面/健康检查均为 200。无真实模型授权/调用与费用验收；客户端切换后仍须自行验证正常对话与流式输出。

## alb-health-20261028-113300

用户授权修复新增 ALB 的后端健康检查，范围仅为 Caddy 健康探测，不切 DNS、不切业务流量。ECS 安全组由用户放行 B/C 交换机网段的 TCP 80；Caddy 在原 `@outside` 拒绝之前增加 `@alb_health`：只有 TCP 来源属于 `172.25.48.0/20` 或 `172.28.192.0/20`、Host 为 `gomoai.ai`、方法为 GET、路径恰为 `/health` 时，反代原后端 `127.0.0.1:8080`。返回真实后端健康响应，不以固定 200 掩盖后端故障。这两个网段内的其他工作负载也能探测这个健康地址，但不能因此访问业务/管理路径。

原 Cloudflare 来源白名单、可信转发头、网站/供应商门户和直连 HTTPS 模型入口均保留。ALB 普通业务请求仍拒绝；当前不是 ALB 业务接入完成，主站 HTTP 80 监听、主站 Cloudflare 来源核验、灰云 API 白名单转发及真实 IP 链路需要后续独立验证后再切换。无应用代码、镜像、数据库或凭据变更，未执行真实模型调用。

隔离网络命名空间运行原生 Caddy HTTP 路由及本地 mock 后端，BASELINE/MODIFIED/ROLLBACK 各 25 项用例通过；同输入 ALB GET `/health` 返回 403→200→403，修改后管理路径、错误 Host/方法/路径和非 ALB 来源仍为 403，Cloudflare 网站/门户及回环健康行为保留。回滚脚本只在修改副本实际执行，恢复 SHA256 与原件一致；补丁重建字节一致。首次校验因 `runuser` 无法读取 `/dev/stdin` 退出 1，已改为独占临时文件校验，原错误及后续通过结果保留在验证记录中；未为测试回滚生产。

语法检查后热加载 Caddy，容器 ID 未变化。20 秒只保留方法/状态计数的生产观测发现两个可用区来源，420 个 GET 健康请求对应 420 个 HTTP 200；控制台最终健康状态仍待用户刷新确认。主站/供应商门户 HTTPS 为 200，现有 API 管理路径为 404、无凭据模型列表为 401。没有修改 DNS 或监听。

原配置 SHA256：`a8318d8fe0d6b36452ec5fccd187583b1f5d45990686b98987565952181452d1`；修改后：`b9a17506bfd4f79f12bad8b51770265f8deb1636f6d458add9e58ed559c55b14`。服务器四角色位于 `/opt/sub2api/releases/alb-health-20261028-113300/`：`MODIFIED_FILE.Caddyfile`、`DIFF_FILE.patch`、`VERIFICATION.txt`、`ROLLBACK.sh`；另有原件 `BASELINE.Caddyfile` 和隔离测试脚本/输出。脚本接受一个目标副本路径，只恢复配置字节、不自动重载；生产回退须先核验现场未被后续修改，再恢复、校验并热加载。不得覆盖后续发布配置。

### 同一事务续作：ALB 业务转发（2026-10-28）

用户已在 ALB 为 HTTP 80、HTTPS 443 配置同一服务器组并确认健康正常，仅 HTTP 80 关联 `cloudflare-only` 非空白名单（15个官方 IPv4 网段），HTTPS 443 不关联该名单。用户授权继续 Caddy 业务转发及真实 IP 适配，仍不修改 DNS、监听、安全组、应用镜像/进程或数据库。

Caddy 保留原 Cloudflare 直达网站/门户路径。新增 ALB→ECS:80 的业务准入：来源限两个既定交换机网段，主站 Host 仅在 ALB 写入 `X-Forwarded-Proto: http` 时允许，API Host 仅在其为 `https` 时进入共享模型白名单，其余 API 路径404。主站不能通过开放的 ALB 443 伪造 Host 绕过 HTTP 80 的 Cloudflare 白名单；ALB 必须继续覆盖写入监听协议头、追加实际 TCP peer 至 XFF，且 HTTP 80 白名单不得关闭或清空。交换机网段属于可信边界，其中其他工作负载同样具有网络层准入能力，不能把不可信工作负载放入这些网段后继续沿用该信任假设。

`trusted_proxies` 仅增加这两个 ALB 网段，保留 strict 右向解析，使用 XFF 而非可由灰云客户端伪造的 CF-Connecting-IP 来推导身份；转发给应用的 XFF、X-Real-IP、CF-Connecting-IP 都重建为已解析身份，删除 Forwarded/True-Client-IP，并保持外部协议https。原直连443仍使用 TCP peer，不因共同信任配置改用客户端头。两个模型入口复用 `model_api_routes`，模型路径、后端认证、计费及流式协议不变。

生产修改前 SHA256 为上述健康检查版本 `b9a17506bfd4f79f12bad8b51770265f8deb1636f6d458add9e58ed559c55b14`；业务版本为 `363fe4b5c12f2a7a60e50290717cd842fdb3405ad0bf1b27f8d642ccbce83086`。相同四角色继续使用，`HEALTH_*` 保存上一版配置/补丁/回滚脚本；`DIFF_FILE.patch` 为最初原件至最新配置的累计补丁，`BUSINESS_DIFF.patch` 为健康版本至业务版本补丁。`ROLLBACK.sh TARGET_COPY business` 恢复本次前健康版本，省略 business 则恢复整个事务最初原件；两种都在独立副本实际执行并比对hash，不能为测试回退生产。原验证账本追加保存，不覆盖早先结果。

原生 Caddy 在无外网网络命名空间、mock后端验证业务修改前/修改后/本次回滚/事务最初回滚，各44例通过。模型路径403→401→403（401由mock认证端返回），健康探测在本次三阶段均200；主站经CF+ALB链路及供应商管理后端归属、真实IP和伪造头、原直连语义、错误Host/协议/路径均检查，模拟SSE完整通过，不执行真实模型调用。线上两个 ALB EIP 不改DNS指定地址验证模型列表401、管理员/供应商管理路径404、伪造主站Host403，即使客户端伪造XFP为http也无法绕过；非CF来源80均超时。现有主站、门户200，现有直连API未认证401、后台404，容器ID不变。

尚未切换 Cloudflare 解析；主站 Cloudflare→ALB 的真实正向完整链路需要切换后立即验收，切换前仅隔离模拟验证该链路，不为测试临时解除白名单。后续切换应先保留原 A/CNAME 值与代理状态，使用 ALB DNS 名称而非固定单个EIP，主站橙云/API灰云逐项切换和验证，异常回退原解析。模型实际授权调用、真实SSE及账务本次未验收。ALB 证书到期前需签发并更新关联；切换后旧 ECS TLS-ALPN 续签可能不可达，不把旧证书自动续期当作长期回退保证。

### 用户完成 DNS 切换后的验收（2026-10-28）

用户在 Cloudflare 控制台将 `gomoai.ai` 和 `api.gomoai.ai` 从原 A `47.236.9.72` 改为 CNAME `alb-toxl2lpoa6gzibub5e.ap-southeast-1.alb.aliyuncsslbintl.com`，主站保持橙云、API保持灰云；`www` 继续 CNAME 到主站。操作者为用户，agent只读验收，没有代改DNS。主站回退值为原 A/橙云，API回退值为原 A/灰云；DNS传播及缓存使回退不保证瞬时生效，且长期回退仍需先确认旧直连证书有效。

使用一次性唯一查询标记发起匿名、不计费探针：主站200、主站 `/api/v1/auth/me` 未认证401、www200、供应商门户200；API模型列表未认证401、管理路径404，API实际连接 ALB 公网IP而非旧ECS。主站响应为Cloudflare动态回源而非缓存命中。只提取本次探针的逐跳路由头，确认 Cloudflare→ALB私网来源→ECS:80→回环应用的真实路径：主站传入协议http，Caddy转给应用时为https，XFF链中保留的真实探针客户端被正确重建为 XFF/X-Real-IP，未把ALB或CF节点误认成用户。API经另一可用区ALB到达应用，后台路径没有转给应用。

同目录 `DNS_CUTOVER_VERIFICATION.json` 保存六项实际结果及只属于本次匿名探针的路由证明；原始抓包、Cookie/授权头、无关客户请求和响应体不保留。`VERIFICATION.txt` 已追加本次证据并继续沿用原四角色，生产配置hash仍为业务版本，未重载/重启或增加配置变更。入口与鉴权链路已验收；真实授权模型调用、真实长时间SSE、消费及并发负载仍未验收。模型客户端须使用API子域名才能绕过Cloudflare代理读取超时，继续调用主站模型接口仍经过Cloudflare；ALB或上游自身超时并未因此无限延长。


## live-20261029-092123-c1e85f20d250

用户授权用原版 live-release.ps1 发布干净提交 `c1e85f20d25020b2f6b87f00af9176feae1bc6b5` 至既定 ECS live，两组件镜像均已切换，脚本未修改、零overlay、无skip、无迁移。供应商 Groups 忽略已删除主站分组的404，其他错误继续返回；本次未编辑业务源码。原 Cloudflare→ALB/Caddy 和 API 灰云入口保持不变。

发布安全6测试、供应商后端284通过事件/0跳过、门户124测试、主站i18n3测试及两套类型/嵌入构建通过；两库dump在无网络PG实际恢复，schema主296/供应商13不变。两容器精确revision、healthy、0重启、仅回环端口。主站/门户健康和页面200、8项公网静态资源hash匹配；管理员登录/供应商列表及4概览200、未认证401；API模型未认证401、后台404。未跑主站完整后端套件或真实模型/OAuth/付款。

备份 `/opt/sub2api/releases/live-20261029-092123-c1e85f20d250/before`，回退入口同目录 `ROLLBACK.sh`。环境副本实际恢复exit0/hash一致，未为测试回退生产或降级DB。持久四角色沿用 `C:/Users/tirol/.codex/artifacts/sub2api-deploy/20261026-live-script`；同输入源码探针BASELINE exit1→MODIFIED exit0→ROLLBACK exit1，恢复hash一致。新发布完整记录在仓库外同名sub2api-live-releases目录。无commit/push。


## live-20261029-095457-79505865500a

用户授权使用原版 live-release.ps1 发布固定提交 `79505865500acff2e971dcd606f40b63549909f0` 至既定ECS live主站/供应商；原有未提交部署记录保留，零源码overlay、未改脚本、无skip/迁移。新发起的供应商接入默认并发20/RPM100，前后端及留空兜底一致；已有账号/授权参数快照不改写，旧空快照及旧路径保留原行为。未修改全局主站默认值、DNS/ALB/Caddy/权限/依赖。

发布安全6项、供应商后端292通过事件/0跳过、门户125测试及两套类型/嵌入构建通过。两库dump实际隔离无网络恢复，schema主296/供应商13不变。两容器精确revision、healthy0重启、仅回环端口；公网健康/页面200、8静态资源hash匹配；管理员登录/列表/4概览200、未认证401；API模型401/后台404。未跑主站完整后端套件或真实模型/OAuth/付款。

备份 `/opt/sub2api/releases/live-20261029-095457-79505865500a/before`，回退入口同目录ROLLBACK.sh；环境副本实际恢复exit0/hash一致，未回退生产/降级DB。沿用原持久四角色，探针BASELINE exit1→MODIFIED exit0→ROLLBACK exit1、恢复hash一致，补充记录在仓库外同名发布目录；无commit/push，项目地图/开发约定不变。
