# balance：100条线下标注草案

回答占位符需要后续fixture真值填充，非真实执行结果。所有条目尚非独立审核金标。

| ID | 原话 | 正确下一步（草案） | 允许工具 | 期望回答/事实模板 | 缺参或备注 |
|---|---|---|---|---|---|
| balance-090 | 把余额给我 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-015 | 我还有多少钱啊 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-038 | 可用余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-069 | 我还能调多少次接口 | 必要追问 | get_balance | 请说明模型、预计用量或预算/时间范围，才能判断余额够用多久或能调用多少次。 | workload_model_and_budget_or_time_horizon; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-046 | 余额还剩多少了 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-056 | 我打算再买点额度，先看看现在还剩多少，免得充多了浪费 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-005 | 余额查询 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-050 | 看下余饿 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-037 | 多少钱 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-021 | 我账上还有余额吗 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-017 | 钱还够不够用啊 | 必要追问 | get_balance | 请说明模型、预计用量或预算/时间范围，才能判断余额够用多久或能调用多少次。 | workload_model_and_budget_or_time_horizon; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-035 | 账户余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-051 | 是这样的，我昨天用你们的服务跑了一批任务，现在想确认一下账户里还剩多少钱，方便我决定要不要充值 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-002 | 查一下账户余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-049 | 账户于额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-060 | 最近没怎么用，想看下余额有没有变化 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-078 | 看下我的 credit 余额 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-026 | 帮我瞅瞅还剩多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-083 | 劳驾帮我查下余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-011 | 查询我的账户余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-047 | 结余多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-093 | 余额给我看 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-039 | 我的余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-100 | 快速查下余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-003 | 我要查余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-001 | 查询余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-033 | 余额多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-066 | 还能撑几天 | 必要追问 | get_balance | 请说明模型、预计用量或预算/时间范围，才能判断余额够用多久或能调用多少次。 | workload_model_and_budget_or_time_horizon; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-091 | 立刻查余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-029 | 我还有多少可用额度 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-079 | 请问可以帮我查一下余额吗 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-036 | 还剩多少 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-048 | 帮我插一下余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-088 | 请问账户还有余额吗 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-018 | 里头还有钱吗 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-089 | 余额报一下 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-082 | 可以帮忙查询一下我的余额吗 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-034 | 看余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-052 | 你好，我最近在用你们平台，想了解一下我当前的账户余额情况，因为我要做个预算规划 | 必要追问 | get_balance | 请说明模型、预计用量或预算/时间范围，才能判断余额够用多久或能调用多少次。 | workload_model_and_budget_or_time_horizon; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-087 | 麻烦您帮我查询余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-022 | 还剩多少额度可以用 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-081 | 能不能帮我看一下账户余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-085 | 方便的话帮我查一下余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-064 | 需不需要充值了 | 必要追问 | get_balance | 请说明模型、预计用量或预算/时间范围，才能判断余额够用多久或能调用多少次。 | workload_model_and_budget_or_time_horizon; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-071 | 帮我查下 balance | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-074 | account balance 是多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-006 | 帮我查账户余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-092 | 马上告诉我余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-004 | 看一下我的余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-096 | 直接告诉我余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-013 | 账户里还有多少钱 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-084 | 请问我的余额是多少呢 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-061 | 我这个月还能跑多少任务 | 必要追问 | get_balance | 请说明模型、预计用量或预算/时间范围，才能判断余额够用多久或能调用多少次。 | workload_model_and_budget_or_time_horizon; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-031 | 余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-072 | check 一下我的余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-040 | 余额查询下 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-058 | 帮我看看账户情况吧，主要想知道还剩多少钱可以用 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-076 | 查下 available credit | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-025 | 我还有钱吗 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-030 | 账上余额还多吗 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-023 | 我这个月还能花多少 | 必要追问 | get_balance | 请说明模型、预计用量或预算/时间范围，才能判断余额够用多久或能调用多少次。 | workload_model_and_budget_or_time_horizon; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-098 | 给我查余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-055 | 因为要报销，我需要知道我现在账户里还剩多少额度，麻烦帮我查查 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-068 | 账上还宽裕吗 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-024 | 卡里还有多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-053 | 麻烦问一下，我想知道我这个账号现在还有多少可用余额，之前充的钱好像用了一部分了 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-028 | 看看够不够再跑几次 | 必要追问 | get_balance | 请说明模型、预计用量或预算/时间范围，才能判断余额够用多久或能调用多少次。 | workload_model_and_budget_or_time_horizon; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-014 | 查询可用余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-065 | 我的预算还剩吗 | 必要追问 | get_balance | 请说明模型、预计用量或预算/时间范围，才能判断余额够用多久或能调用多少次。 | workload_model_and_budget_or_time_horizon; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-099 | 余额发我 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-045 | 查佘额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-041 | 查鱼额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-063 | 我够不够再买一个模型 | 必要追问 | get_balance | 请说明模型、预计用量或预算/时间范围，才能判断余额够用多久或能调用多少次。 | workload_model_and_budget_or_time_horizon; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-077 | 我的 balance 还有多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-086 | 能否告知我的账户余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-095 | 查！快点 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-020 | 看看我还能用多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-010 | 查看余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-012 | 余额还有多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-043 | 余鹅多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-097 | 余额亮出来 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-057 | 刚才调用接口好像扣费了，我想确认一下现在账户余额还剩多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-009 | 我想知道账户余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-042 | 查雨额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-054 | 我这边需要核对一下账目，请帮我查一下当前账户的余额是多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-070 | 没超支吧 | 必要追问 | get_balance | 请说明模型、预计用量或预算/时间范围，才能判断余额够用多久或能调用多少次。 | workload_model_and_budget_or_time_horizon; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-032 | 查余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-094 | 报一下当前余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-075 | 我想看下 quota 剩余 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-007 | 账户余额是多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-080 | 麻烦帮我查查余额好吗 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-067 | 我现在的消费能力还剩多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-062 | 钱用完了吗 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-059 | 我老板让我确认下我们账户的余额，麻烦查一下 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-019 | 我现在手头还剩多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-027 | 我这点钱还够用吗 | 必要追问 | get_balance | 请说明模型、预计用量或预算/时间范围，才能判断余额够用多久或能调用多少次。 | workload_model_and_budget_or_time_horizon; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
| balance-008 | 查我的可用余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-016 | 我这账户里还剩多少 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-044 | 帮我差一下余额 | 查询并回答 | get_balance | 当前可用余额为¥{balance_yuan}。 |  |
| balance-073 | 查 credit 还有多少 | 必要追问 | 无直接工具要求 | 您想查询现金余额、套餐剩余额度，还是其他信息？ | account_measure_or_query_target_and_context; 原始独立文本不足以唯一确定工具；不能把生成器类别当用户上下文。 |
