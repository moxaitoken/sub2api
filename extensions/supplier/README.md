# 供应商模块

目标以 [R2架构](docs/architecture.md) 和 [详细设计](docs/phase-1-technical-design.md) 为准。当前完成的是 **R2 D1身份与管理端**，不是供应商模块全部功能。

## 当前可用

- 供应商：管理员创建的本地登录账户/密码；服务端Cookie/CSRF、版本/epoch检查与退出。无需主站用户或邮箱。
- 平台管理员：独立登录入口，复用主站真实admin/profile/refresh/已有2FA；不要求绑定供应商。可分页搜索、开户、账户资料、启停和受控重置密码。
- 开户同事务写供应商、密码散列、零余额、持久幂等和实际管理员审计。名称/账户唯一性和输入校验在服务端，密码不回显。
- 停用/重置撤销旧会话；重新启用不会复活旧会话；不删除供给/历史，停用供应商的资源仍可由管理员查询。
- 供应商仅三个业务菜单；管理员仅供应商列表和财务管理。可查看限定supplier的真实账号列表。代理、用量、财务和账号写入当前明确标为未接入，不展示演示数据。

当前登录页面：[本地供应商](http://127.0.0.1:5174/)、[本地管理员](http://127.0.0.1:5174/?portal=admin)。登录入口只选择认证流程，不能切换权限。测试凭据保存在私有运行目录的R2_LOGIN.txt和LOGIN.txt，不放入仓库。

## 当前API

供应商前缀 `/api/supplier/v1`：POST auth/login（login_account/password）、POST auth/logout、GET me、GET accounts。me为principal/supplier/csrf_token。

管理员前缀 `/api/supplier/admin/v1`：auth/login、auth/login/2fa、auth/logout、me；GET/POST suppliers、GET suppliers/{id}、PUT suppliers/{id}/status、POST suppliers/{id}/credential-reset、GET suppliers/{id}/accounts。me没有supplier，写操作Origin+CSRF，变更带If-Match与幂等键。

旧members/ops/review及旧账号写路由不在生产组合中注册。旧协议测试通过显式LegacyTestMode保留历史回归，但main不启用此模式，也没有环境开关。

## 数据与验证

复用原有服务、库、sessions/operations/audit。001–005不可变；006增加suppliers凭据/epoch、会话类型约束、供应商会话审计和余额表，并撤销历史本地登录会话。旧映射与资源保留，不自动迁移密码或启用pending账号。服务启动只检查schema，DDL须显式-migrate，禁止指向主站库。

密码使用Go标准库PBKDF2-HMAC-SHA256、600000轮、随机16字节salt，格式带算法和参数，不新增依赖。主站管理员Token仍加密且刷新前有持久屏障。真实部署需匹配专用运行角色权限；迁移角色不作运行账号。

```sh
# 模块backend：现有匹配Go工具链
 go test -count=1 ./...
 go vet ./...
 go build ./cmd/server
# 仓库根：只读挂载源码、无公网无端口一次性PG；输出须在仓库外
 python extensions/supplier/tests/integration/run_backend_checks.py --output EXTERNAL_DIRECTORY
# frontend
 pnpm run build
```

项目内refresh_local_portal.py仅更新已标记的本地测试环境，迁移前备份；smoke_r2_identity.py验证真实本地主站和模块的开户→登录→停用→启用→重置→退出，创建空资源合成供应商。禁止使用生产配置或测试生产。

下一步按方案完成代理基础，再开放账号接入/修改、真实用量和财务结算。真实Claude授权、自动授权、PRD全部AC、正式浏览器及生产发布尚未完成。当前开发结果见 [R2实施记录](docs/r2-implementation-results.md)。
