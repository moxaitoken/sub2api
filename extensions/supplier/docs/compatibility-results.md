# 首期隔离验证结果

日期：2026-10-22。对应 [详细设计](phase-1-technical-design.md) 的 S0 阶段。

本文件保留S0当时的验证事实；后续S1实现另记于 [s1-implementation-results.md](s1-implementation-results.md)，不回写历史测试为已通过。

**结论：详细设计和参考表约束已验证；G1/G2/G3 尚未全部通过，不能据此开始生产接入或宣称模块已经开发完成。**

## 1. 环境与不可变边界

- 仓库 HEAD：`a0d8370a13243ffdb136a2522a082b4164b8c0ad`，分支 `deployment-supplier-plan`。
- 本地 Sub2API 镜像：`sub2api-local:b12696932788-local`，ID `sha256:a771e119e9e3f0e228e552de90c9c31e7342a3a5b90cbee45019088a8d6d5ea3`。镜像源码与当前 HEAD 之间仅有4个部署文件变化，backend/frontend/Dockerfile没有差异；并非本轮重新构建的镜像。
- Docker Engine：29.1.3；PostgreSQL测试镜像：18-alpine；Redis测试镜像：7.4。均为本地已有镜像，运行使用 `--pull=never`。
- 应用探针使用独立 internal Docker 网络及一次性PG/Redis，最终通过容器内HTTP访问，不发布公网端口。表约束探针使用 `--network none`，无端口映射、无现有卷，数据仅在tmpfs。
- 没有连接生产主机/数据库，没有读取生产密码，没有请求Claude服务，没有接受管理员合规声明。各探针结束后均按自建标签核验并清理了自己的容器/网络；未清理他人资源。
- 原始 `architecture.md` 字节未变，SHA256：`041ade8919c487ae53346a6af4ac1822539619c0369e1430307fac4ea5e08173`。

## 2. 已执行的验证

| 检查 | 观察结果 | 退出状态/结论 |
| --- | --- | --- |
| 六部分详细设计结构 | 表结构/ER图、权限、接口、失败处理、同步算法、22项A类验收用例均存在 | 校验器0；只验证文档结构，不验证服务功能 |
| 参考SQL实际执行 | 创建11张表、索引和约束 | psql 0 |
| 唯一账号映射及身份指纹 | 重复归属、重复身份被唯一约束拒绝 | 测试脚本0（捕获预期错误） |
| 代理归属/实例隔离 | 跨供应商或跨实例代理绑定被复合FK拒绝 | 测试脚本0 |
| 首期平台及准入字段 | 非Claude、未审批desired_state=running被CHECK拒绝 | 测试脚本0；不等于真实网关隔离通过 |
| 同一资源操作并发 | 两连接竞争；第一连接成功，第二连接违反 `supplier_one_inflight_resource`，最终仅1行 | 第一0、第二预期3；并发断言通过 |
| 墓碑及用量历史 | 硬删除受RESTRICT保护，软删除保留6条用量，旧ID不能另归属 | 测试脚本0 |
| 账号费用 | 6组NULL/零/非默认倍率得到10、0、0、6、20、2 | 测试脚本0；固定样本非真实业务对账 |
| SQL基线/变更/回滚/重应用 | 表数依次为0 → 11 → 0 → 11；样本事务回滚后各表无样本残留 | 各检查0；仅一次性库 |
| 原样OAuth辅助源码测试 | URL/PKCE参数、内存会话过期、删除、读取不消费、独立store不共享，共8项 | go test 0 |
| 完整backend模块测试预检 | 本地Go1.23.3低于go.mod要求1.27.2；设置GOTOOLCHAIN=local，不下载工具链 | 退出1，未运行完整模块测试 |
| 本地应用HTTP | `/health`、登录、`/user/profile`、`/admin/compliance`均200 | HTTP探针成功，不是账号接入成功 |
| 管理员初始化门控 | compliance.required=true，version=v2026.06.10；`GET /api/v1/admin/accounts`返回423 | wget预期退出1；探针整体0表示成功观察阻断 |

OAuth辅助测试将上游 `oauth.go`、原测试和新增辅助测试按原样复制至外部临时目录运行，校验副本与源文件SHA相等；没有为了适配本机Go版本修改上游模块或源码。该结果不能代替 `OAuthService.ExchangeCode`、真实授权或建账号测试。

## 3. 三个开发前门槛

| 门槛 | 当前状态 | 尚缺的观察 |
| --- | --- | --- |
| G1 新建账号与隔离准入 | **阻断：本地管理员合规门控423** | 显式隔离组创建、默认/fallback/复合路由反例、创建全过程无生产路由命中 |
| G2 OAuth与两阶段恢复 | **部分辅助测试通过；完整路径阻断** | 真实HTTP换码/建号、幂等重放/过期、响应丢失与补偿；真实Claude授权还需明确授权的测试账号 |
| G3 用量同步 | **参考费用样本通过；HTTP分页路径阻断** | 实际接口跨账号/日期分页、追加/迟到/删除及统计核对 |

人工确认入口属于隔离测试实例的管理员合规流程；没有调用accept接口或通过SQL、缓存、JWT等方式绕过。下一步先由实际管理员阅读并确认隔离实例中的承诺，再复跑G1/G2/G3；需要真实Claude测试时再提供专用测试账号，不使用生产账号代替。

在这些门槛通过前不将S2账号接入开放给供应商。S1/S2/S3的完整业务代码和生产发布本轮均未进行，不把参考DDL当已安装的模块迁移。

## 4. 错误、修正与证据

保留的失败不是测试通过：

1. Docker初次未启动；启动已安装的Docker Desktop后恢复，仅为本地环境变化。
2. internal网络上的应用没有可用host published端口，`docker port ... 8080/tcp`退出1；改用容器内HTTP，没有扩大公网暴露。
3. HTTP辅助函数遮蔽 `http.client` 导致退出1；修正命名。nc方法出现请求提前取消/429，随后等待超时；改用镜像已有wget，最终得到200/423结果。先前失败均保留，不解释成应用权限已通过。
4. PostgreSQL18的ON DELETE RESTRICT实际SQLSTATE为23001；首版测试误期望23503，修正断言后重跑通过，未改DDL规避约束。
5. 文档补丁首次受Git autocrlf影响，精确字节比对失败；修正为 `git -c core.autocrlf=false apply`，随后基线/变更/回滚/重应用和原文哈希检查通过。

机器相关日志在仓库外：`C:\Users\tirol\.codex\artifacts\sub2api-supplier-phase1\20261022\`。

- `probes/isolated-e8skqyrl/REPORT.json`：8项辅助测试及Go版本阻断。
- `probes/runtime-1e2173b0/REPORT.json`：原始port错误。
- `probes/runtime-internal-612d8378/REPORT.json`：最终HTTP门控结果与前序失败索引；临时认证值脱敏。
- `schema/REPORT.json`：逐命令stdin/stdout/stderr/退出码、表结构hash及并发测试。
- `VERIFICATION.txt`：总证据索引、文档副本基线/变更/回滚记录和文件hash。

测试入口及复现边界见 [compatibility README](../tests/compatibility/README.md)。日志和真实运行配置不进入源码库。
