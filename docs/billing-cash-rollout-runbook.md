# 现金计费修复：切换、核对与回退

本手册对应 `02a226b` 的现金锁与恢复协议；验收证据见 [2026-10-18 本地结果](billing-release-readiness-2026-10-18.md)。本轮没有执行生产部署。

**当前决策：暂缓生产放量。** 本地资金断言通过，但单租户 32 并发的完整周期吞吐由约 204 降至 6.6/s，P99 约 5 秒。以下是后续候选版本通过性能验收后使用的操作步骤，不表示当前版本已获上线认可。

后续候选 `2f16bd0` 已减少重复查账，10秒窗口及历史账本对比见[性能报告](billing-cash-performance-2026-10-18.md)。资金回归通过，单租户排队与提交成本仍明显；该结果不改变暂缓放量决策，也未改变本手册的现金锁切换协议。

## 1. 放行条件与准备

- 确定待发布的完整 Git SHA，保存二进制 SHA256、构建版本和配置版本；不要在切换过程中直接 `git pull` 并运行不确定版本。
- 先为实际租户流量和峰值并发确定可接受的吞吐、P95/P99、锁超时率及观察窗口。当前本地短时结果不满足“可以认定生产容量充分”的条件；性能优化后需同探针复测，再做足够时长和账本规模的环境验收。
- 保留之前的资金故障回归：重建与预扣/结算/冲正交错、旧 SQL 会话失联、退款投影失败后新消费、guard 过期、取消和单连接恢复。不能通过移除核对、租户锁或 fencing 换取压测成绩。
- 核对所有写入者连接同一预期 MySQL 实例和 Redis 实例/DB。MySQL 会话锁属于服务器会话，本协议不适用于把同一租户的连接随机分配到互不共享锁的 MySQL 实例；不同进程也不能指向不同的余额 Redis。
- 盘点 `billing`、`agent`、`reconciler`、定时任务、脚本和管理入口。`cmd/agent` 直接构造 Store，部分概览 GET 也会修复余额，不能只升级 billing。event-consumer 只写审计，但其版本、消费组和 offsets 仍需保留。
- 在停写窗口保存 MySQL 一致性备份，包含 tenants、topups、recharge_orders、bills、outbox 及关联配置；记录 Redis 持久化/备份状态。确认可以恢复，备份不作为覆盖切换后新账目的快捷回退手段。

构建验证命令（本地已执行构建；不启动服务）：

```powershell
New-Item -ItemType Directory -Force -Path .tmp/billing-readiness-bin | Out-Null
go build -trimpath -o .tmp/billing-readiness-bin/ ./cmd/billing ./cmd/agent ./cmd/reconciler ./cmd/event-consumer
Get-ChildItem .tmp/billing-readiness-bin/*.exe | Get-FileHash -Algorithm SHA256
```

正式发布从批准的干净代码版本重新构建；不要把本轮忽略目录中的本地构建直接当作已验收发布包。连接凭据和 `CONTROL_PLANE_TOKEN` 沿用环境的安全注入，不写入报告或命令输出。

## 2. 协调停止旧版现金写入

1. 使用现有入口维护机制阻止新模型请求、新充值/退款、agent 写任务和可触发余额重建的查询。仓库没有统一的“现金只读”开关，不能只设置一个假定存在的环境变量。
2. 让既有请求完成上游流、`ReportUsage` 和终态结算；同时观察在途数量。不要把仍在生成内容的请求当作无用量账单强制冲正。
3. 暂停后台 sweep/rebuild 和所有直接 Store 调用者；确认请求已排空后，按实际服务管理器或明确 PID 停止旧 billing/agent 实例。不要用模糊进程名批量杀进程。
4. 核对全部旧实例确已退出，不再有来自旧实例的 SQL 操作或 Redis 写入。按目标租户查询 `IS_USED_LOCK(CONCAT('billing:cash:', id))`、检查会话及持有者；锁消失只能作为佐证，旧版本本来就可能不取得该锁。
5. 留下切换时间、实例清单、pending 快照及备份标识。业务库中的 pending 可以是此前崩溃遗留，不能以“删掉所有 pending”作为排空方法。

禁止新旧现金协议版本共用同一租户并同时写入。新 token 只能约束遵守 fence 的新版 Lua，不能阻止旧版裸 SET 或旧版未加锁的现金变更。也不要手工删除 guard、owner key 或修改余额来“释放卡住的锁”；MySQL 锁必须由所属会话正常释放或终止所属进程后核验释放。

## 3. 启动新版并在无业务流量时核对

1. 保持入口维护，先启动同一候选版本的 billing；agent 和后台对账暂不接收任务。检查 MySQL/Redis 可达、启动既有 AutoMigrate 成功和 `/healthz`。健康探针只说明进程存活。
2. `EnsureBalanceKeys` 只补缺失 key，不修复已有脏余额。通过新版 billing RPC 显式执行一轮重建；已有配置安全注入后可用新版 reconciler：

   ```powershell
   & .tmp/billing-readiness-bin/reconciler.exe -once -no-sweep
   ```

   该命令会改写余额投影，应在已确认的目标环境、停业务写入时执行。本轮仅构建，未在业务库执行。既有 CLI 遇到个别租户 RPC 错误可能记录日志后仍退出 0，必须核对日志没有 `recon rebuild` 错误、rebuilt 数与活跃租户数一致，并进行下面的独立检查，不能只看退出码。
3. 在选定目标业务库的只读 SQL 会话执行账本快照，金额单位均为分：

   ```sql
   SELECT t.id,
          t.initial_quota + COALESCE(u.amount, 0)
            - COALESCE(p.pre, 0) - COALESCE(s.actual, 0) AS ledger_cent
   FROM tenants AS t
   LEFT JOIN (
     SELECT tenant_id, SUM(amount) AS amount FROM topups GROUP BY tenant_id
   ) AS u ON u.tenant_id = t.id
   LEFT JOIN (
     SELECT tenant_id, SUM(pre_quota) AS pre FROM bills
     WHERE phase = 'reserve' AND status = 'pending' GROUP BY tenant_id
   ) AS p ON p.tenant_id = t.id
   LEFT JOIN (
     SELECT tenant_id, SUM(actual_quota) AS actual FROM bills
     WHERE phase = 'settle' AND status = 'settled' GROUP BY tenant_id
   ) AS s ON s.tenant_id = t.id
   WHERE t.status = 0 ORDER BY t.id;
   ```

   使用已认证的 Redis 运维会话确认 `PING` 成功，并逐个核验上述活跃租户的 `billing:balance:<id>` 存在、可读且等于 `ledger_cent`。缺 key、Redis 读取错误与余额为零必须区分；不要将读取失败当作 0。保存核对结果而不保存凭据。
4. `/admin/reconcile-check` 是额外的只读核对入口，要求 HTTP 成功、`ok=true`、`checked` 等于库内全部租户数量（包含停用租户；前述重建仅针对活跃租户）。停用租户的既存差异应另行核查，不能假定本轮重建也会修复它们。该接口不是 Redis 可达性或全量逐 key 核验的替代。示例仅在已配置的目标管理地址执行：

   ```powershell
   $cashCheck = Invoke-RestMethod -Uri ($env:BILLING_ADMIN_BASE + '/admin/reconcile-check') -Headers @{'X-Control-Token'=$env:CONTROL_PLANE_TOKEN}
   if (-not $cashCheck.ok -or $cashCheck.checked -le 0) { throw 'Cash reconciliation check failed' }
   ```

   另外在同一目标库只读执行 `SELECT COUNT(*) FROM tenants`，将结果与 `checked` 比较，必须完全相等；示例的 `checked > 0` 只防止空结果，不能取代此检查。`BILLING_ADMIN_BASE` 是本命令使用的运维变量，需要预先设置真实地址；服务本身使用 `BILLING_ADMIN_PORT`。核对期间保持无现金写入，否则分开读取 SQL/Redis 的快照差异不能直接视为账目丢失。
5. 对崩溃遗留 pending，先确认没有存活上游流，再按既定宽限期执行 sweep；可独立运行 `reconciler.exe -once -no-rebuild -grace 600`，不要为追求 pending=0 任意缩短宽限期。无 usage marker 的账单冲正，有 marker 的补结算。逐条检查日志和账单状态，因为 CLI 同样可能记录个别失败后返回 0。完成后再重建、重复独立资金核对。

## 4. 逐步恢复与中止条件

全部现金写入实例必须已统一到通过性能验收的新版本。先恢复小规模指定租户流量，再按此前批准的容量边界扩展；观察完整计费周期延迟、每租户排队、`cash lock`/`request lock timed out`/`stale cash owner` 错误、业务失败率及资金核对。

出现不一致、反复投影修复失败、持续锁超时、尾延迟或吞吐突破批准边界，立即停止新增现金请求并保留现场。`stale cash owner` 是旧持有者被拒绝的保护信号，持续出现需要调查会话断连和并发进程，不能通过跳过 token 检查来消除日志。

充值/退款 API 返回“已入账、投影待恢复”时，先查询已有订单/账本，只使用原订单号或原幂等键重试。不要生成新幂等键再退款，也不要删除 topup 流水、回滚订单状态或手工加钱。已完成请求的快速重放可能暂时返回旧 Redis 余额；应按账本和明确的恢复流程核对，不凭该展示值创建补偿账目。

## 5. 回退与恢复服务

1. 再次阻止新现金操作，排空或记录在途请求，暂停 agent、对账及直接 Store 写入者。
2. 保存故障版本、日志、SQL/Redis 核对结果和新增已提交流水。备份后新提交的订单、账本与 outbox 必须保留；不能用切换前数据库或 Redis 备份直接覆盖它们。
3. 首选切换到已验证且仍遵守租户锁、fencing、保留账本协议的安全版本，按第 3 节恢复投影并核验。
4. 如果仅有 `5054b58` 等旧协议二进制可回退，现金功能继续维护停写；旧版不得接回现金流量。其余功能只有在确认不会调用 Store 修复余额或执行现金变更后才能独立恢复，不能把“GET”一概视为只读。
5. 没有可用安全版本时保持现金入口关闭，修复后重新执行本手册验收。`02a226b` 具备已验证的资金保护但存在本轮测到的性能回退，不能无条件充当满足容量的回退版本。

## 6. 本轮核验记录

已检查上述命令使用的真实 CLI 参数、管理路径、令牌 header、账本公式以及 billing/agent 的 Store 初始化入口；四个进程的源码构建验证结果和独立审查记录见验收报告。生产切换、备份恢复演练、容量 SLO、长时间压力和真实模型/JEV付费评测均未在本轮执行。
