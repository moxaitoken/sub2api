# S1 登录与单供应商身份：实现及验证

日期：2026-10-22。仓库基线HEAD：`a0d8370a13243ffdb136a2522a082b4164b8c0ad`；本轮未提交、未推送、未发布生产。

## 本次精简后的范围

用户2026-10-22明确要求去掉未确认的辅助功能：一个供应商对应一个登录账号，管理多个Claude账号。已移除多人角色、成员管理、供应商开通及关联的运营API/页面；`/me`仅返回`user,supplier,csrf_token`，`/members`、`/ops`及子路由为404。主站admin不自动获得供应商访问资格。

只保留服务端私有一一身份映射和安全隔离。既有表名`supplier_members`及role=owner是兼容内部列，不是可配置产品角色。历史迁移001～003保留，004增加supplier_id唯一和固定role约束；不兼容多成员/其他角色数据使迁移失败并回滚，不自动删改。真实映射初始化方式待接入阶段明确，不新增开户运营入口。

不新增或强制2FA，仅兼容主站已有挑战，避免绕过既有认证。账号审批/平台准入仍保留为S2业务，不因移除运营门户而改为自动审批。ClaudeOAuth、账号池、全部账号用量和代理均未在本次完成，模块未部署。

独立Go后端、Vue前端、加密服务端会话、Cookie/CSRF、实时上游身份/状态校验、精确Origin、IP/UA绑定和外部消费屏障保留；主站源码及部署配置不改。详细接口见[README](../README.md)。

## 本次精简复验

- 后端最新单元/真实隔离PG集成、vet、build逐命令结果：`C:\Users\tirol\.codex\artifacts\sub2api-supplier-s1\20261022\simplification\backend\VERIFICATION.json`，不沿用下方历史测试数量。
- 本次设计检查保留六个交付章节、关系图、简化权限矩阵及22个业务验收规格；检查通过只说明设计结构，不表示核心业务已完成。
- 当前参考DDL及约束已在无网络、无发布端口、tmpfs的一次性PG18容器执行通过；新增同供应商第二登录身份拒绝、同上游用户跨供应商绑定拒绝、非固定role拒绝用例。样本事务已回滚、该临时容器已删除。原始输出：`C:\Users\tirol\.codex\artifacts\sub2api-supplier-s1\20261022\simplification\docs\schema-results.json`。
- 前端与整体回滚补充记录由本次外部事务报告保存，不能用旧构建结果替代本次验证。以上均不代表生产或真实Claude验收。

## 精简前S1历史记录（不代表当前功能）

以下安全修正与测试结果为本次精简**之前**实际发生的证据。曾实现运营开通/成员关联/角色变更的路径现已删除；保留历史原始日志，不将旧测试数量冒充本次复验，也不宣称当前仍有这些功能。精简后的新验证由本次外部事务报告记录。

## 安全复核与修正

- 最初在会话行锁内读取成员会另借连接，可能耗尽连接池；已改为提交会话事务后读成员，真实PG+HTTP测试使用MaxOpenConns=1通过。
- 刷新/2FA外部消费后如果本地提交失败，旧凭据可能被重放；已在外调前持久Checkpoint。专用连接的advisory锁跨Checkpoint提交持有，失败后的external_pending或已撤销挑战保留。未知结果强制重登，不重放旧Token。
- Checkpoint释放行锁期间的并发停用，使用重新读回+版本CAS+不可撤销反转保护，测试在准确竞争边界验证拒绝继续外调/不复活。
- 已知刷新结果在后续profile临时失败时仍保存，避免丢失已旋转的refresh token。
- HTTP创建的幂等重放移到目标用户查询之前；同键原成功后目标读取503仍重放原成功。真正的当前操作人身份仍每次校验，不能用重放绕过运营权限。
- 适配器固定源站和路径，禁止跳转、Cookie转发、环境代理，限制超时/响应大小，只公开安全错误；423等状态不被变成成功。

## 精简前历史执行结果

| 命令/场景 | 观察 |
| --- | --- |
| Go1.27.2隔离容器 `go test -count=1 -timeout=60s -json ./...` | 34个顶层测试、86个PASS事件（含表驱动子用例），无测试被skip，exit0 |
| `go vet ./...` | exit0 |
| `go build -o /tmp/supplier-server ./cmd/server` | exit0；仅构建，没有启动生产服务 |
| PG18隔离库：顺序执行repository、server的integration测试 | 35个PASS事件（含子用例及包内单测），无skip，exit0 |
| HTTP+真实模块PG+合成上游 | 运营开通 → owner登录 → 实际持久成员列表 → 换服务实例会话仍有效 → 停用撤会话，全部通过 |
| 创建结果重放 | 上游目标用户查询模拟503后，同key仍返回201及原供应商，不再创建 |
| 故障/并发 | refresh串行、未知pending不重试、2FA挑战单次消费、ctx取消后屏障保留、Checkpoint竞争停用、last owner、CAS和账本；通过 |
| `pnpm run build` | 24项前端测试、typecheck和Vite构建通过，exit0 |
| 独立lock frozen offline安装 | 已验证通过；根前端lock未变化 |
| 源码边界 | 当时原architecture.md字节和已跟踪主站源码/配置未变；本次已授权同步精简架构与设计，不再宣称原架构字节未变 |

测试使用已加载Go/PostgreSQL镜像、本地专用测试库，无发布端口、无生产卷。固定pq依赖按现有版本下载进入专用Go缓存卷；最终Go验证关闭GOPROXY/GOSUMDB。没有读取生产凭据、连接生产或调用Claude。历史实际HTTP上游由httptest合成，**不能据此宣称真实主站所有认证设置、验证码、双域名和真实隔离环境权限已验收**。

已知修正过程也保留：首次服务器测试有未使用import导致编译失败，删除该测试import后重跑通过；前端首次offline安装缺包、锁文件裁剪别名/空snapshot问题修正后frozen复验通过；报告输出遇到终端GBK编码问题仅影响打印，不影响UTF-8文件证据及测试结果。

## 仍未完成（当前边界）

- S2账号接入/审批、S3用量/代理、S4部署均未启用；对应API为404，不返回假成功。
- S0的G1/G2/G3真实管理接口仍需实际管理员在隔离实例处理合规门控；没有自动accept、改库或复用生产数据绕过。
- 真实Claude OAuth还需明确授权的专用测试账号；验证码真实组件、真实浏览器端到端、生产DB角色授权和容量压测未验收。
- S1支持单实例部署起点；登录限流位于单进程，跨实例统一限流需要后续容量/部署设计。会话及刷新串行化已使用PG跨进程锁，不依赖内存锁。
- 历史失败账本和会话当前没有自动清理，避免未经确认的数据保留期被擅自执行；上线前落实保留策略及清理验证。

原始逐命令stdout/stderr/exit及文件hash保留在仓库外：

- `C:\Users\tirol\.codex\artifacts\sub2api-supplier-s1\20261022\root\FINAL_REPORT.json`
- 同目录 `final-backend.json`、`final-postgres.json`、`final-frontend.json`
- `C:\Users\tirol\.codex\artifacts\sub2api-supplier-s1\20261022\repository\VERIFICATION.txt`
- 原有四角色文件继续位于 `C:\Users\tirol\.codex\artifacts\sub2api-supplier-phase1\20261022\`，本轮证据作为补充，不替换S0结果。
