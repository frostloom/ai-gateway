# bills：100条线下标注草案

回答占位符需要后续fixture真值填充，非真实执行结果。所有条目尚非独立审核金标。

| ID | 原话 | 正确下一步（草案） | 允许工具 | 期望回答/事实模板 | 缺参或备注 |
|---|---|---|---|---|---|
| bills-098 | 给我查账单明细 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-097 | 账单亮出来 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-074 | 拉一下 billing 明细 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-005 | 账单查询 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-027 | 帮我看看最近扣的钱 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-063 | 哪次调用扣的钱 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-020 | 我这几笔都扣了啥 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-019 | 给我看看最近的记录 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-033 | 流水 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-018 | 最近有扣费吗 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-094 | 报一下最近账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-082 | 可以帮忙查询一下最近的消费记录吗 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-014 | 查最近的流水 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-038 | 我的账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-032 | 查账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-085 | 方便的话帮我查一下账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-054 | 我想看看最近这几笔调用分别扣了多少钱，方便定位哪次用量异常 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-096 | 直接列出来 | 必要追问 | 无直接工具要求 | 请说明要查询的对象或具体记录。 | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| bills-056 | 我这边发现余额少了一些，想看看最近的账单明细确认一下扣费情况 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-030 | 把最近的记录拉一下 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-015 | 我最近都买了啥 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-002 | 查询账单明细 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-036 | 消费记录 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-060 | 最近有几笔扣费我记不清了，麻烦帮我查一下明细 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-055 | 麻烦帮我查一下最近的消费流水，我想逐笔核对一下金额 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-092 | 马上拉账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-013 | 账单明细查询 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-016 | 看看我最近几笔单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-022 | 我上次调用扣了多少 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-044 | 涨单明细 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-058 | 我需要最近的账单流水来做报销凭证，麻烦帮我导出一下 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-004 | 查看最近几笔账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-086 | 能否告知我最近的扣费记录 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-061 | 我余额怎么少了 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-007 | 查询我的账单流水 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-084 | 请问我的最近账单在哪看呢 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-049 | 帐单查询 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-083 | 劳驾帮我查下账单明细 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-078 | 查下流水 transaction | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-090 | 把账单给我 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-043 | 帮我差账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-024 | 最近的单子给我瞅瞅 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-047 | 最近帐单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-062 | 刚才那笔算成功了吗 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-066 | 帮我看看扣费对不对 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-045 | 查帐单记录 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-026 | 最近花了几笔 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-034 | 最近账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-076 | 看下 payment 记录 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-028 | 我最近消费明细呢 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-052 | 你好，我需要查看最近的账单明细来做对账，能帮我拉一下吗 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-089 | 账单报一下 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-067 | 有没有重复扣费 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-003 | 我要看账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-048 | 查看涨单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-035 | 明细 | 必要追问 | 无直接工具要求 | 请说明要查询的对象或具体记录。 | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| bills-023 | 看看我最近都干了啥 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-037 | 账单明细 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-029 | 最近几单扣多少 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-072 | 看下最近的 order 记录 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-064 | 有异常扣费吗 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-041 | 查帐单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-095 | 查账单，快点 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-051 | 我想核对一下最近的调用记录，麻烦帮我查一下最近几笔扣费的明细 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-057 | 请帮我把最近十笔的消费记录列出来，我要做成本分析 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-088 | 请问可以看下我的账单明细吗 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-025 | 我这两天扣费记录 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-017 | 我昨天那笔扣了多少 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-080 | 麻烦帮我看看最近账单好吗 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-011 | 查询最近十笔账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-046 | 账单留水 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-009 | 我要查账单记录 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-068 | 这笔钱扣得对吗 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-010 | 查下账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-039 | 看流水 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-069 | 我账户变动记录 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-040 | 扣费记录 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-077 | 我的 order 列表 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-006 | 帮我查最近的消费记录 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-008 | 看一下最近的账单明细 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-021 | 最近扣了几次钱 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-093 | 账单明细给我 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-031 | 账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-071 | 查下 bill 明细 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-053 | 因为财务要核对，我需要最近这段时间每一笔扣费的记录，麻烦查一下 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
| bills-050 | 流水记路 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-042 | 账单明细查下 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-012 | 看看我的账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-070 | 钱是怎么花出去的 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-099 | 流水发我 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-001 | 查一下最近的账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-065 | 我这几天的钱怎么没的 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-100 | 快速查下账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-081 | 能不能帮我拉一下账单明细 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-091 | 立刻查账单 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-075 | 帮我查 recent bills | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-079 | 请问可以帮我查一下账单吗 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-073 | 查 transaction 记录 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-087 | 麻烦您帮我查一下账单流水 | 查询并回答 | get_recent_bills | 最近{requested_limit}笔已结算消费：{actual_bill_details}。 |  |
| bills-059 | 帮我看看最近几笔单子，我想确认下有没有异常扣费 | 查询并说明能力边界 | get_recent_bills | {actual_recent_bill_details}；如需特定订单、完整时段或导出，请补充范围；当前最近消费工具不覆盖全部订单/导出。 | 最近已结算消费最多10笔，不等于充值订单、全部流水、原始调用状态或导出功能。 |
