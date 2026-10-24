# 账号池可读取指标

## 范围与口径

供应商账号池与主站管理员供应商详情复用同一资源组件及处理器。本轮只补已有可靠来源：

| 字段 | 来源与含义 |
| --- | --- |
| 并发上限 | 主站账号 `concurrency` 配置，不是当前请求并发 |
| 调度优先级 | 主站账号 `priority`，数值越小越优先；不冒充独立权重或费率 |
| 今日消耗 | 供应商库已同步 `total_cost`，按 Asia/Shanghai 自然日汇总 |
| 累计消耗 | 该账号模块内保留的全部已同步官方标准消耗，不乘用户分组/账号倍率，不等于已入账美分余额 |
| 已记录请求数 | 用量统计既有 `record_count`；仅落库用量记录，失败/重试尝试可能不包含 |

实时 RPM、TPM、不含缓存 TPM、实时并发、高负载及 Claude 5h/7d/Fable 配额仍未接入。不会用配置值、日累计或模拟数据代替。此文只说明代码与隔离测试，不代表真实 Claude 非零消费或生产发布验收。

## 兼容接口

既有供应商 `GET /api/supplier/v1/accounts` 与管理员 `GET /api/supplier/admin/v1/suppliers/{id}/accounts` 支持可选 `include_metrics=1`。未请求时保持原响应；分页、身份和归属校验不变。新前端可兼容旧服务器缺少 `metrics`。

每条账号增加可选只读字段：

```json
{"metrics":{"configuration":{"concurrency_limit":100,"priority":50},"consumption":{"today_cost_usd":"0.100000000000000001","total_cost_usd":"0.300000000000000001","today_status":"complete","total_status":"partial","observed_at":"2026-10-24T04:00:00Z"}}}
```

- 配置或消耗读取失败时对应对象为 `null`，原账号列表仍可用；金额没有已观测来源时为 `null`，页面显示 `—`，不能伪造 0。
- 金额为精确十进制字符串，页面不转 JS Number。已观测的零显示 `$0`。
- 未完成历史发现、游标缺失、gap/重试或过期扫描标记 `partial`，保留已获取金额并展示提示。
- 今日完整需今日已对账且扫描不早于 90 秒；全部历史还要求历史发现完成、历史起点至今日每天游标齐全，今天/昨天 90 秒、其他日 48 小时内完成对账，沿用用量统计刷新边界。
- `observed_at` 是投影查询时间，不是最后同步时间，也不保证实时。累计仅限模块已有数据；来源保留造成缺口时不得称为完整历史。

## 边界、性能与失败

`accounts.EnrichAccounts` 仅对模块列表已确定的本供应商账号 ID 读取；浏览器不能指定任意主站账号。消费 SQL 单语句快照，同时限定供应商、实例及账号，排除软删除账号。配置适配器只解码主站单账号接口的 ID/平台/类型/并发/优先级，返回白名单配置，不转发 credentials、客户信息、Key 或主站资源 ID。

消费查询预算 3 秒；主站读取共用 4 秒预算、最多 4 路并发，每页最多沿用现有 100 条上限。取消请求终止后续读。页面刷新为按需读取，没有额外定时轮询、独立服务或缓存；大历史聚合可能超时并显示不可用，实际规模下仍需观测延迟再决定汇总优化。

不改主站计费/调度、不直连主库、不加迁移、不变更权限。无数据库回滚；恢复旧模块/前端代码即可停止读取投影（既有 013 等迁移兼容限制不变）。代码副本回滚不回退生产数据。

## 验证入口

- 模块 Go 单元/适配器：`go test ./internal/accounts ./internal/adapters/sub2api`。
- 项目隔离工具 `extensions/supplier/tests/integration/run_backend_checks.py --output EXTERNAL_PATH`：一次性 PostgreSQL，SQL 精度/日界/缺口、HTTP 归属与失败路径；不连接生产。
- 供应商前端 `pnpm run build`；主站前端 `pnpm run typecheck` 及供应商视图 Vitest。
