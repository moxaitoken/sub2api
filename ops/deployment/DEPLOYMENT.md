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
