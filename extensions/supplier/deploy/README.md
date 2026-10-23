# 供应商生产部署

2026-10-23，用户授权将最新代码发布到现有ECS。应用源码固定为 `3b9cbbd6d0b26ed7926842e69de935f93aa271ee`；发布补丁补齐主站Docker共享组件依赖、供应商镜像/Compose和Caddy管理API路由。未提交或推送本次配置补丁。

## 当前结构

- 主站镜像 `sub2api-local:3b9cbbd6d0b2-release`，原数据卷保留，宿主机仅 `127.0.0.1:8080`。
- 供应商镜像 `sub2api-supplier:3b9cbbd6d0b2-release`，包含Go服务与门户静态产物；非root、只读根文件系统、仅 `127.0.0.1:8091`。
- 主站 `https://gomoai.ai/admin/suppliers`、`/admin/supplier-finance` 可用。Caddy将 `/api/supplier/admin/v1/*` 转到供应商服务，其他路径仍转主站。
- 门户 `https://goccmax.com` 已接通：Cloudflare代理及有效边缘证书、强制HTTPS；Caddy转发供应商服务，www跳转根域名。回源仍为用户选择的Flexible/HTTP，非端到端加密。
- 复用已有RDS实例，新建独立 `sub2api_supplier` 数据库。`supplier_migrator`拥有模块DDL，`supplier_runtime`仅必要DML，无DDL或历史财务UPDATE/DELETE权限。001–010显式迁移，HTTP启动只校验。
- 主站与模块通过 `172.30.91.0/29` 私网通信：主站 `.2`，模块 `.3`。部署前确认网段不冲突。主站额外只信任模块 `.3/32` 和实际宿主机桥接地址；模块只信任宿主机 `.1/32`。不得改成整段信任。
- 主站托管PG/Redis、JWT/TOTP密钥、Cloudflare Flexible和安全组保持原配置。新增模块独立加密密钥和仅服务端管理Key，不输出秘密。
- 隔离组 `supplier-quarantine`（ID 3）为Anthropic、exclusive、inactive且无API Key；本次未批准任何正式供给分组（`SUPPLIER_APPROVED_GROUP_IDS`为空），未更改已有供给账号的调度状态。

## 文件与命令

宿主机 `/opt/sub2api/production.env`包含主站配置及两个镜像标签；`supplier.env`包含模块运行配置，`supplier-migration.env`仅供显式迁移；均0600。模板见 `../backend/config.env.example`，不得把实际值放进仓库或终端输出。

镜像从固定Git导出加已记录的发布补丁构建，避免将本地秘密、node_modules和调试产物放进上下文：

```sh
docker build --platform linux/amd64 --build-arg COMMIT=SOURCE_SHA -t MAIN_IMAGE .
docker build --platform linux/amd64 --build-arg COMMIT=SOURCE_SHA -f extensions/supplier/deploy/Dockerfile -t SUPPLIER_IMAGE .
```

服务器先备份两个数据库，再以迁移身份执行，应用运行身份不得承担迁移：

```sh
docker run --rm --network host --env-file /opt/sub2api/supplier-migration.env SUPPLIER_IMAGE -migrate
# 随后以迁移角色连接sub2api_supplier执行runtime-grants.sql。
docker compose -p sub2api --env-file /opt/sub2api/production.env \
  -f /opt/sub2api/compose.production.yml -f /opt/sub2api/compose.supplier.yml up -d
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
systemctl reload caddy
```

服务器 `compose.supplier.yml`对应本目录 `compose.production.yml`。后续发布必须保留两个Compose文件和既有项目名，禁止 `down -v`。API和门户前端必须与模块schema匹配，禁止替换旧迁移内容。

## 验收与回退

本次验证：供应商Go单元/vet/build及隔离PG集成通过，无命名测试跳过；门户91项测试及类型/构建通过；主站供应商管理30项测试及类型/构建通过。线上主站登录、健康、供应商列表/财务读取200；无认证/无效Bearer401；错误Origin写请求403。两个容器healthy，0重启；原主库296迁移、3用户、2账号的备份已在同ECS无网络临时PG中实际恢复验证。

备份位于 `/opt/sub2api/backups/release-20261023-3b9cbbd6/`，包含发布前主库、首次模块迁移前库、应用数据、原env/Compose/Caddy。`ROLLBACK-release.sh`恢复旧主站镜像和入口并停模块，保留模块数据库及所有业务历史。该生产回退脚本仅做语法检查，本次未为测试而回退在线服务。Caddy副本回退已实际验证：同请求BASELINE 404 → MODIFIED 401 → ROLLBACK 404，恢复哈希一致。

010以后不能只换回旧供应商二进制；旧程序会拒绝未知迁移。应使用兼容010的修复版本，或在专门维护流程中恢复匹配备份；不得删计提事件、改历史SQL或自动逆转已登记结算。

正式供给分组策略、真实Claude授权/非零调用及财务业务验收仍待完成。当前部署验证不代表这些业务链路已验证，也未创建测试供应商、登记付款或触发真实模型调用。


## 2026-10-23 门户入口补充发布

用户要求发布另一agent完成的管理员入口移除：仅重建并替换供应商镜像 `sub2api-supplier:login-20261023-151328`，主站镜像/容器、数据库schema、鉴权及Caddy保持不变。供应商登录页不再显示管理员链接，现有管理员兼容API未删除；本次不扩大为后端能力删除。94项门户测试、类型检查和构建通过；隔离副本同测试BASELINE 2失败/4通过 → MODIFIED 6通过 → ROLLBACK 2失败/4通过，源哈希恢复一致。线上浏览器确认链接消失，静态资源200、未登录API401、主站健康200。未提交、推送，未创建业务账号或执行真实登录/模型/财务验收。

该次发布备份和独立回退脚本位于 `/opt/sub2api/releases/login-20261023-151328`。`ROLLBACK.sh`无参数只回退供应商镜像env及容器，保留域名路由；带一个参数可恢复env副本（已实际执行并核对哈希）。真实生产回退未为测试执行。初次全量发布的历史备份仍保留。


## release-20261023-155948

用户授权发布工作区最新代码（基线 `8f66492cae56757b66215cb8810e51834d0a8641` 加 source-manifest.json 记录的已核对改动）。镜像：主站 `sub2api-local:release-20261023-155948`，供应商 `sub2api-supplier:release-20261023-155948`。本次发布Goccmax品牌及供应商名称/账户/密码分字段错误提示，保留密码强度/原样传递、鉴权和数据库schema；无迁移、无依赖升级，不创建生产供应商或触发模型/财务写入。

门户101测试、类型/构建，主站34相关测试及类型/i18n/构建，供应商后端单元/vet/build和隔离PG集成（232通过事件、0跳过）通过。分字段测试同输入BASELINE/ROLLBACK均为旧通用错误（预期exit1），MODIFIED具体字段错误通过（exit0），源副本恢复hash一致。

生产备份 `/opt/sub2api/releases/release-20261023-155948/before`，含两个库dump、data、env/Compose及Caddy；两个dump在无网络临时PG中实际恢复验证。`/opt/sub2api/releases/release-20261023-155948/ROLLBACK.sh` 无参数恢复该次旧镜像env和两个容器，保留数据库与路由；一个参数恢复指定env副本（已实际测试hash一致）。生产回退未为测试执行。主站登录、供应商管理/财务读取、鉴权拒绝、两个健康检查及公网门户/资源通过，浏览器工具被Work设置禁用，未做本次渲染或真实供应商登录验收。
