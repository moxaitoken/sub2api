# 首期兼容验证

这些测试是设计期的局部验证，不是供应商应用的完整测试套件。禁止指向生产数据库、使用生产.env或真实Claude凭据。

## 文档结构与OAuth内存辅助测试

在仓库根目录执行（Python标准库、已有Go；输出必须在仓库外）：

```sh
python extensions/supplier/tests/compatibility/verify_design.py extensions/supplier/docs/phase-1-technical-design.md
python extensions/supplier/tests/compatibility/run_isolated_checks.py --output /ABSOLUTE/EXTERNAL/ARTIFACTS
```

第二个命令退出0只代表8项辅助测试及副本完整性通过；REPORT中 `all_three_gates_passed` 仍为false。若backend模块所需Go工具链不可用，原始失败单独记账，不修改go.mod，也不自动下载。

## 参考SQL

当前参考DDL与本次精简一致：每供应商只允许一个登录映射，单一上游用户不能绑定多个供应商，role仅保留固定owner兼容列。schema_constraints.sql新增对应三项拒绝用例；历史S0报告不因此改写。实际运行迁移001～003仍不可变，由004前向收紧，不能用参考DDL替换迁移。

仅在新建的一次性、无网络、无现有卷的PostgreSQL18中运行。下述 `CONTAINER` 必须是本次创建并已核对隔离标签的容器，不能是共享实例：

```sh
docker exec -i CONTAINER psql -X -v ON_ERROR_STOP=1 -U postgres -d postgres < extensions/supplier/docs/phase-1-schema.sql
docker exec -i CONTAINER psql -X -v ON_ERROR_STOP=1 -U postgres -d postgres < extensions/supplier/tests/compatibility/schema_constraints.sql
```

SQL测试用事务回滚样本；约束违反被预期捕获，不是把任何异常都当通过。两连接并发及schema回滚/重应用由外部 `schema/run_schema_checks.py` 实际执行，原始命令和输出保留在该次REPORT。

参考DDL不执行权限创建或生产迁移，也不验证真实网关路由、OAuth换码或HTTP分页。完整G1/G2/G3需要隔离应用联调；若遇到423管理员合规门控立即记录，不能自动accept或改库绕过。

详细结果见 [compatibility-results.md](../../docs/compatibility-results.md)。
