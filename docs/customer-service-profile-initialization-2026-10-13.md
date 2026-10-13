# 六种合成账户的真实初始化验证

日期：2026-10-13。范围是账户初始化与状态核验，不是客服会话运行或模型质量测评。

## 实际完成

使用真实本地MySQL、新建随机名测试库、私有miniredis替身，验证六个本人账户和六个外租户对照。新库在测试结束时删除，并查询information_schema确认不存在；没有操作开发业务库或共享Redis。启动了已有ai-gateway-mysql容器，未启动模型客服服务。

| profile | 现金余额（元） | 持久化订阅 |
|---|---:|---|
| no_subscription | 5000 | 无 |
| active_standard | 5000 | 月度·标准，active，自动续费 |
| active_flagship | 5000 | 月度·旗舰，active，自动续费 |
| cancelled_standard | 5000 | 月度·标准，cancelled，停止续费，本期到期日在未来 |
| low_balance | 25 | 无 |
| empty_balance | 0 | 无 |

核验独立租户和测试key、本人已支付订单、Redis钱包与权威账本一致、订阅身份/起止时间/续费状态、近7天现金消费2.50元、最近3/10笔账单的顺序/时间/输入输出token/扣费，以及窗口额度。订阅reserve与settle同一用量只算一次，文本档消耗3000token；零现金消费在最近账单可见。外租户对照余额1234元且账本一致，本人查询没有混入其记录。

这是每种模板一次实际初始化，不是已创建1220个任务账户，也不是运行3071个完整对话分支。六种账户持久化核验通过，不代表所有客服业务语义通过。

## 整数充值的独立修正层

旧冻结profile按“目标余额+250分”定义种子充值，但真实CreateRechargeOrder只接受整数元。这轮另建`eval/runtime-profiles/2026-10-13-six-profiles-v1`，不覆盖旧归档：

```text
tenant.initial_quota = 50分（明确合成初始额度）
paid_order.amount_cent = 目标余额 + 200分
paid_order.amount_money = amount_cent / 100（整数）
钱包与账本终值 = 50 + amount_cent - 250 = 目标余额
```

充值建单/支付、订阅/退订通过真实store方法完成。现金与订阅历史账单是隔离库的直接合成种子，不证明真实reserve/settle链路或支付网关已测试。种账单后按权威账本重建私有Redis投影。余额、消费、订阅及原任务退款终态预期保持一致；订单金额的修正由新overlay明确声明并绑定源SHA。

## 已发现的业务阻塞

cancelled_standard在数据库中保留正确的取消状态、未来本期到期日及额度计算基础；但当前GetActiveRecurring只筛active，MeOverview返回订阅为空、额度为空。与“退订只停下期续费、当期继续使用”的约定存在差异。

测试分别断言持久化记录、直接SubscriptionAllowance计算及真实MeOverview返回；输出明确标为KNOWN CONTRACT GAP。测试通过表示差异被验证和记录，不表示问题已修复，也不证明当期计费资格仍有效。生产代码本轮未更改。推进完整客服测试前，需要修复/明确已取消但未到期订阅的展示与额度资格，保留独立复现和升级兼容证据。

## 复核入口

```powershell
python scripts/prepare-runtime-customer-profiles.py verify
go test ./internal/taskbench -run TestSixRuntimeProfiles -v -count=1
```

本地本次六profile集成测试非skip通过（测试本体3.40秒），包含真实清理确认。无MySQL环境会显式skip，不能将CI的skip当成真实初始化证据。所有导出清单的database_instantiated_during_export=0只描述纯文件导出；上述数据库测试另外记录。

模型/JEV调用为0，未更新历史1220条输出或分数。完整会话runner适配和独立语义评分仍待完成。最终回归证据见[面试记录](interview/CHANGELOG.md)。
