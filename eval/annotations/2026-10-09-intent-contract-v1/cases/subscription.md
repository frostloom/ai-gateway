# subscription：100条线下标注草案

回答占位符需要后续fixture真值填充，非真实执行结果。所有条目尚非独立审核金标。

| ID | 原话 | 正确下一步（草案） | 允许工具 | 期望回答/事实模板 | 缺参或备注 |
|---|---|---|---|---|---|
| subscription-065 | 我的额度刷新了吗 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-074 | 我的 subscription 状态 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-016 | 我的套餐还有效吗 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-063 | 我的会员还有效吗 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-092 | 马上查套餐 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| subscription-049 | 查顶阅 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-059 | 我需要知道我现在的套餐订阅还剩多少天到期 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-004 | 查询订阅详情 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-078 | 我的 quota 刷新了吗 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-060 | 帮我查下我的订阅信息，我想看看这个周期用了多少额度 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-098 | 给我查我的订阅 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-088 | 请问我的订阅还有效吗 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-057 | 我想了解一下我这个套餐的额度刷新时间是多久，以及现在还剩多少 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-040 | 剩余额度 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| subscription-091 | 立刻查我的订阅 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-019 | 我买的套餐还能用多久 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-076 | 我的 plan 什么时候到期 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-037 | 套餐剩余 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| subscription-099 | 套餐剩余发我 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-055 | 帮我查一下我当前订阅的套餐名称和这一期的额度使用情况 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-090 | 把订阅状态给我 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-079 | 请问可以帮我查一下我的订阅吗 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-039 | 订阅周期 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-006 | 查我的套餐订阅 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-075 | 查我的 allowance | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| subscription-026 | 我订的套餐还剩多少 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-034 | 我的套餐 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-013 | 看下我的订阅详情 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-097 | 订阅状态亮出来 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-100 | 快速查下我的订阅 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-024 | 我这包月还有几天 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-071 | 查下我的 subscription | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-048 | 我的订阋还剩多久 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-080 | 麻烦帮我看看订阅状态好吗 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-042 | 我的订阋 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-036 | 订阅详情 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-009 | 查看我的套餐 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-021 | 我这订阅啥情况 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-027 | 我现在的套餐是哪个 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-032 | 查订阅 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-082 | 可以帮忙查询一下我的订阅详情吗 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-030 | 我套餐啥时候刷新 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-070 | 我的额度还有多少没用 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| subscription-012 | 查询订阅周期 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-083 | 劳驾帮我查下我的订阅 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-003 | 我的订阅情况 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-002 | 查看我的订阅状态 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-067 | 我这期用超了吗 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-052 | 你好，我之前订了一个套餐，想查一下它现在的状态和到期时间 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-023 | 我现在是啥套餐 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-062 | 我是不是该续费了 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-053 | 麻烦帮我看看我的订阅详情，包括周期起止时间和剩余额度 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-061 | 我这个月额度还有吗 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| subscription-020 | 我订了啥来着 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-025 | 我的订阅到啥时候 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-022 | 我的套餐过期了没 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-068 | 套餐什么时候重置 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-005 | 看下我的订阅 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-073 | 看下我的 quota 剩余 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| subscription-066 | 我还剩几天 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| subscription-044 | 查下订越 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-064 | 我需不需要重新订 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-093 | 订阅详情给我 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-017 | 我订阅的东西还在吗 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-041 | 查订阪 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-084 | 请问我的套餐还剩多少额度呢 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-086 | 能否告知我的订阅到期时间 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-038 | 我的额度 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| subscription-096 | 直接告诉我套餐名 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-056 | 我不太确定我的订阅有没有自动续费，麻烦帮我查一下订阅状态 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-008 | 查询当前订阅 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-089 | 我的订阅报一下 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-015 | 我订的是哪个套餐 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-046 | 订阅详请 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-033 | 订阅状态 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-047 | 订阅读状态 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-045 | 我的套惨 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-095 | 查订阅，快点 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-029 | 我的订阅续费了吗 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-010 | 查下我的订阅 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-014 | 我的订阅状态查询 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-011 | 我的订阅还有多久 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-081 | 能不能帮我查下我的套餐 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-054 | 我想知道我的套餐订阅是否还在有效期内，以及什么时候需要续费 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-058 | 请帮我确认当前的订阅情况，我要决定是继续订还是改套餐 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-035 | 看订阅 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-094 | 报一下我的套餐 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-043 | 订阋状态 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-087 | 麻烦您帮我查下我的订阅 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-031 | 我的订阅 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-050 | 套惨剩余 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-028 | 我这期额度还有吗 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-069 | 我还在订阅中吗 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-051 | 我想确认一下我当前的订阅套餐是什么，以及这一期的额度还剩下多少可以用 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-077 | 看下 subscription 详情 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-001 | 查询我的订阅 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-072 | 我的 plan 还有效吗 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-018 | 我这套餐什么时候到期 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-085 | 方便的话帮我查一下订阅情况 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
| subscription-007 | 我的订阅信息 | 查询并回答 | get_my_subscription | {actual_subscription_and_requested_details}。 |  |
