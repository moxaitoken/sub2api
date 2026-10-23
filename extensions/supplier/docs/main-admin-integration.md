# 主站供应商管理接入

2026-10-23：用户确认将供应商平台管理放入 Sub2API 主站后台，并授权本地实施。

## 验收与边界

- 主站管理员一次登录，可进入供应商列表与财务入口，沿用主站布局、主题、语言。
- 主站真实 admin 才可见菜单、通过页面守卫及调用管理接口；普通用户、供应商身份不得越权。
- 迁入既有搜索、分页、开户、资料、启停、密码重置、指定供应商账号读取。业务仍在供应商服务，保留现有幂等键、版本锁和实际管理员审计。
- 供应商列表展示实际余额；账号池、代理和用量进入锁定供应商的完整详情页，共用供应商门户组件。管理员可执行已有手动接入、代理新增/检测/删除，仍保留新账号暂停与归属校验。
- 财务支持已完成线下付款的登记、余额扣减、结算记录与流水查询。采购计提未配置，不能把官方用量价计入应付；不写演示余额。
- 保留独立供应商门户和既有 Cookie/CSRF API。无主库直连、上游 Ent 变更、新依赖或服务；新增供应商模块迁移009。生产、提交与推送不在本次范围。

## 接入方式

主站路由 `/admin/suppliers`、`/admin/supplier-finance` 使用 `requiresAuth` 与 `requiresAdmin`；业务视图位于 `frontend/src/views/admin/supplier/`，主站仅导航、路由和语言文件接入。

`frontend/src/api/admin/suppliers.ts` 复用主站 apiClient，使用固定同源 `/api/supplier/admin/v1` 路径，继承 Access Token 与刷新逻辑，不传递 refresh token，不在 URL、日志或新存储中保存凭据。

供应商服务逐请求用调用者 Token 向固定主站 `/api/v1/user/profile` 获取最新身份，要求 active/admin，再用同一 Token 查询 `/api/v1/admin/compliance` 并要求明确 required=false。请求失败、未知响应、身份降级、停用和 Token 撤销均拒绝访问。主站会话 IP/UA 校验继续生效；代理链必须只信任实际中间节点，保留规范化客户端 IP 和 User-Agent，不能靠放宽鉴权修复链路。

Bearer 认证不创建独立会话，不自动确认合规。页面收到合规阻断时唤起主站既有确认界面。供应商本地 Cookie 登录不变。已有独立管理员 Cookie 会话保留原行为兼容，受原 Origin/CSRF 校验约束。

管理业务接口显式携带 Authorization 时只走 Bearer；无效 Bearer 不回退 Cookie。无 Authorization 的已有调用继续使用 Cookie/CSRF。两种身份路径不混用，机器管理密钥只用于服务端资源适配，不用于证明浏览器访问者的身份。

`SUPPLIER_ADMIN_ORIGIN` 指定主站 origin；仅管理业务 Bearer 写请求可使用该 origin，不能用于供应商端或旧登录接口。其他写入继续要求 `SUPPLIER_PUBLIC_ORIGIN`。仅允许 HTTPS；显式本地模式允许 loopback HTTP。不提供跨站 CORS。

## 本地和发布

主站 Vite 将 `/api/supplier/admin/v1` 转发给 `VITE_DEV_SUPPLIER_TARGET`（默认 `http://127.0.0.1:5174`），其他 `/api` 转发 `VITE_DEV_PROXY_TARGET`。本地入口绑定 loopback，供应商服务配置 `SUPPLIER_ADMIN_ORIGIN` 与主站入口一致。前端构建仍走主站正常输出机制。

当前本地已将原主站后端移至 127.0.0.1:8083，主站 Vite 保留 127.0.0.1:8082，供应商门户保持 127.0.0.1:5174。仅本地运行辅助脚本 extensions/supplier/tests/integration/start_main_admin.py --runtime RUNTIME，其中 RUNTIME 必须是带有匹配标签的既有隔离运行目录；脚本先备份配置和二进制，失败自动恢复，运行状态写入该目录的 main-admin-runtime.json。这个脚本用于从原始端口布局切换一次，不重复执行；后续启动前端可在 frontend 设置 VITE_DEV_PORT=8082、VITE_DEV_PROXY_TARGET=http://127.0.0.1:8083 后运行 pnpm dev --host 127.0.0.1 --strictPort。

发布时由同域入口优先将管理 API 前缀转发供应商服务，其他主站请求仍转发 Sub2API；这只是新增路径，不能覆盖 `/api/v1/admin`。需要同时发布主站前端、供应商后端并配置 origin/可信代理。本轮不修改线上入口、不发布生产。

回退时恢复本次前端/供应商二进制和同源入口配置；009为增量表，保留新增表及已提交财务记录，不自动删表或逆转结算。测试产生的开户、启停和重置是正常持久化业务记录，不由代码回退删除。

## 验证

- 主站前端：供应商组件与 API 测试、实际路由守卫、导航及国际化测试、类型检查和构建。
- 供应商后端：Bearer/现有 Cookie 双路径权限、显式合规状态、失效 Token、账号停用/降级、上游失败、错误 origin；隔离 PG 验证开户、搜索、详情、账号读取、重放、版本冲突、密码重置及实际 actor 审计。
- 本地浏览器：管理员现有登录直接打开新页面，确认导航、主题、弹窗、余额及财务空态；未登录/普通用户路由及接口拒绝分别验证。

## 本轮授权：补齐业务详情与结算

2026-10-23 用户“开始执行吧”批准本地补齐三个业务详情、列表布局、结算及流水。仅本地与一次性数据库；不发布生产、不提交推送、不执行真实付款。独立门户保留三个菜单。采购计提仍待 PRD §11 的业务规则。

- `/admin/suppliers/:id/:tab(accounts|proxies|usage)` 保留主站布局、返回列表和三个页签。目标只从路由 UUID 解析，服务端授权后重新读取该供应商；不存在/无权限失败，不回退到示例或当前供应商。
- 管理资源接口统一位于 `/api/supplier/admin/v1/suppliers/{supplier_id}/...`，复用原资源处理器与服务。`ports.WithAdminActor` 只由管理员鉴权设置，审计记录实际操作人；资源的本地 ownership/幂等操作仍属于目标供应商。请求取消后的清理保留管理员上下文。供应商端接口与 CSRF 不变。
- `POST /suppliers/{id}/settlements`：`Idempotency-Key` 必填 UUID；输入 `amount_usd` 是十进制字符串，正数且最多两位小数；四种 `payment_method`、可选 `reference`(200字)、`remark`(1000字)。201完成、200同请求重放，409余额不足/同键异参；停用供应商仍可结算。
- `GET /finance/ledger?supplier_id=&search=&page=&page_size=`：供应商筛选同时影响概览和流水，关键字只影响流水；同一只读快照。`GET /finance/settlements/{id}` 返回只读详情。无编辑、撤销、调账或付款 API。
- 009复用006余额表，增加 `supplier_settlements` 与一对一 `supplier_finance_ledger`。金额用 bigint 分；服务校验后进入事务，锁管理员幂等键，再锁供应商余额。余额、结算、流水与实际管理员审计原子提交；金额上溢或余额不足拒绝。流水金额与前后余额直接读取不可变结算快照，避免第二份金额产生分歧。
- 前端金额不经过浮点计算。请求结果未知时冻结输入并保留原键重试；明确拒绝后刷新余额。零余额禁用结算。付款方式只是登记信息，既不自动转账也不换汇。
- 历史余额仍保留；新开户零余额。真实采购计提、真实 Claude 非零用量验收仍未完成。

验证：后端 unit/vet/build + 一次性PG的精确金额、并发超支、重放/异参、事务回滚、停用结算、筛选/审计；主站组件/路由/客户端和门户回归、类型/国际化/构建、本地浏览器。可执行命令见项目地图。
