# consumption：100条线下标注草案

回答占位符需要后续fixture真值填充，非真实执行结果。所有条目尚非独立审核金标。

| ID | 原话 | 正确下一步（草案） | 允许工具 | 期望回答/事实模板 | 缺参或备注 |
|---|---|---|---|---|---|
| consumption-019 | 我最近花钱多吗 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-049 | 查下晓费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-047 | 查看销费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-036 | 花了多少 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-030 | 帮我算算花了多少 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-052 | 你好，我做成本核算需要知道最近七天一共消费了多少钱，能帮我查一下吗 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-005 | 查消费记录 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-039 | 我的消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-095 | 查消费，快点 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-093 | 消费明细给我 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-079 | 请问可以帮我查一下消费情况吗 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-061 | 我这个月超支了吗 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-092 | 马上统计消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-063 | 我的钱主要花在哪了 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-027 | 这阵子花了多少 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-058 | 我想复盘一下这个月的支出，能帮我查一下消费记录吗 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-035 | 用量 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-032 | 查消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-077 | 我的 usage 多吗 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-070 | 最近的支出趋势如何 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-010 | 查下消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-021 | 我花了多少啊 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-023 | 我这两天的开销 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-083 | 劳驾帮我查下消费记录 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-090 | 把消费统计给我 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-011 | 查询我的用量消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-082 | 可以帮忙查询一下近期的消费吗 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-009 | 我要查消费总额 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-031 | 消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-089 | 消费报一下 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-022 | 最近消耗怎么样 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-071 | 查下 usage 统计 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-075 | 帮我查 usage 消耗 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-045 | 肖费统计 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-007 | 查询近七天消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-042 | 查看小费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-088 | 请问我的花费统计在哪看 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-038 | 近期消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-094 | 报一下本周消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-008 | 看一下消费统计 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-060 | 帮我拉一下最近的消费数据，我要做个汇报 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-097 | 消费数据亮出来 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-069 | 我这个用量算多吗 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-003 | 我要看消费情况 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-020 | 看看我这两天用掉多少 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-051 | 我想统计一下最近一段时间在这个平台上的总体花费情况，主要是想控制一下成本 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-085 | 方便的话帮我拉一下消费统计 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-014 | 查消费汇总 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-046 | 消费记路 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-065 | 我的花费正常吗 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-016 | 最近用了多少 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-002 | 查询消费统计 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-018 | 这周花销大不大 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-078 | 查下 token 消耗 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-081 | 能不能帮我看看消费统计 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-048 | 消费统记 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-012 | 看看我的消费情况 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-043 | 消费了多少了 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-017 | 我烧了多少钱 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-087 | 麻烦您帮我查一下用量消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-028 | 我消费高不高 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-029 | 最近用得多不多 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-037 | 消费统计 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-004 | 查看消费明细 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-024 | 用掉多少钱了 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-100 | 快速统计下消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-076 | 看下 spending 情况 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-006 | 帮我查这周的消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-074 | 查 cost 统计 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-040 | 消耗情况 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-044 | 帮我差消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-034 | 看消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-054 | 我们团队在用这个平台，我想了解一下近期的消费统计，方便做预算 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-041 | 查消废 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-055 | 我想知道最近一周在模型调用上花了多少钱，可以给我一个汇总吗 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-072 | 看下我的 consumption | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-091 | 立刻查消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-080 | 麻烦帮我统计一下消费好吗 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-086 | 能否告知我的消费情况 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-033 | 消费多少 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-062 | 哪个模型最费钱 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-053 | 麻烦帮我看看最近这段时间我的消费情况，我想对比一下上个月的支出 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-050 | 销费情况 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-068 | 钱都去哪了 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-099 | 消费拉出来 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-098 | 给我查消费统计 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-013 | 消费统计查询 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-064 | 成本控制得怎么样 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-084 | 请问我最近消费了多少呢 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-026 | 我都花哪儿去了 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-056 | 为了控制成本，我需要定期查看消费情况，麻烦帮我查一下最近的 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-073 | 我这周的 cost 是多少 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-067 | 额度用得差不多了吧 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-015 | 我这几天花了多少钱 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-066 | 是不是该省着点用了 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-025 | 最近很费钱吗 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-059 | 最近账单有点多，我想看看总的消费是多少 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-096 | 直接告诉我花了多少 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
| consumption-057 | 麻烦统计一下我近期的消费额度分布，我想看看钱主要花在哪个模型上 | 查询并说明能力边界 | get_consumption | 当前可查询近7天消耗。{actual_supported_result}；{requested_period_or_breakdown_limit}。 | 工具仅支持近7天总览；明确用户要求与能力差异，不能把默认结果冒充完整答案。 |
| consumption-001 | 查一下我的消费 | 查询并回答 | get_consumption | 近7天累计消耗¥{total_yuan}。{requested_breakdown} |  |
