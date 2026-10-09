# recharge：100条线下标注草案

回答占位符需要后续fixture真值填充，非真实执行结果。所有条目尚非独立审核金标。

| ID | 原话 | 正确下一步（草案） | 允许工具 | 期望回答/事实模板 | 缺参或备注 |
|---|---|---|---|---|---|
| recharge-059 | 麻烦给账户充值 50 元，我先试用一下 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-003 | 给账户充值 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-099 | 充 100 元 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-045 | 充只 50 块 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-075 | top up 我的账户 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-086 | 能否帮我给账户充值 100 元 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-013 | 帮我账户充值 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-052 | 你好，我需要给账户充值，金额是 200 元，麻烦帮我操作一下 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-064 | 没钱了怎么办 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| recharge-078 | 帮我 deposit 100 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-098 | 立即充值 100 元 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-051 | 我账户里的余额不太够了，麻烦帮我充值 100 元进去 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-087 | 麻烦您帮我充值 500 元 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-054 | 麻烦帮我充值 100 元，我要用来跑一批模型调用 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-044 | 帮我冲 100 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-037 | 充值 100 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-005 | 帮我充值 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-006 | 我要充钱 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-067 | 我想增加额度 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-010 | 我要充 500 元 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-049 | 充zhi 100 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-076 | 我要 recharge 账户 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-084 | 请问可以充值吗 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| recharge-062 | 怎么往里加钱 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| recharge-069 | 我要买额度 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-017 | 给我加点额度 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-095 | 直接充 100 进去 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-050 | 冲直 100 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-035 | 充 50 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-094 | 给我账户充 300 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-034 | 冲值 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-093 | 充值 100，快点 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-029 | 充个三五百 | 必要追问 | 无直接工具要求 | 您想办理哪项业务？请明确操作和目标。 | amount_yuan; 无上下文/单位/授权不能从原类别推定办理。 |
| recharge-082 | 可以帮忙充值 50 元吗 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-007 | 账户充值 50 元 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-033 | 充 100 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-073 | 充 100 credit | 必要追问 | 无直接工具要求 | 您想办理哪项业务？请明确操作和目标。 | amount_yuan; 无上下文/单位/授权不能从原类别推定办理。 |
| recharge-058 | 我需要补充额度，请帮我充值 200 元 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-065 | 余额不够跑任务了 | 必要追问 | 无直接工具要求 | 您想办理哪项业务？请明确操作和目标。 | amount_yuan; 无上下文/单位/授权不能从原类别推定办理。 |
| recharge-079 | 请问可以帮我充值 100 元吗 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-042 | 冲值 100 元 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-020 | 帮我充 100 呗 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-090 | 给我充 200 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-022 | 我得多充点 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-061 | 我额度不够了 | 必要追问 | 无直接工具要求 | 您想办理哪项业务？请明确操作和目标。 | amount_yuan; 无上下文/单位/授权不能从原类别推定办理。 |
| recharge-008 | 帮我充 200 块 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-043 | 我要充直 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-053 | 我准备继续使用你们的服务，先给账户充 500 块钱 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-096 | 充钱，快点 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-091 | 立刻充值 100 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-056 | 我要给这个账户充值 1000 元，方便后续使用 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-032 | 充钱 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-048 | 给账户冲点钱 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-018 | 卡里没钱了，充一下 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-088 | 请问充值怎么操作 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| recharge-014 | 我要充值额度 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-068 | 能不能加点钱进去 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-066 | 怎么补充额度 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| recharge-092 | 马上充 500 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-011 | 给这个账户充 100 元 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-039 | 充 200 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-004 | 查询充值入口 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| recharge-019 | 我想充个一百块 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-027 | 钱用光了，得充了 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-081 | 能不能帮我充值 200 元 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-083 | 劳驾帮我充 100 块 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-030 | 我想往里充点钱 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-046 | 冲值 200 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-024 | 给我账户充点 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-060 | 我想预充 500 元进去，这样比较方便 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-089 | 充 100 #2 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 | 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| recharge-057 | 帮我充 100 块钱吧，之前的额度用光了 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-025 | 充 50 进去 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-026 | 我先充 200 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-047 | 充植一百块 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-070 | 额度用完了 | 必要追问 | 无直接工具要求 | 您想办理哪项业务？请明确操作和目标。 | amount_yuan; 无上下文/单位/授权不能从原类别推定办理。 |
| recharge-009 | 充值操作 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-085 | 方便的话帮我充 300 元 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-016 | 帮我充点钱 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-097 | 给我充 50 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-041 | 充植 100 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-015 | 我钱不够了，充点 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-040 | 来 100 | 必要追问 | 无直接工具要求 | 您想办理哪项业务？请明确操作和目标。 | amount_yuan; 无上下文/单位/授权不能从原类别推定办理。 |
| recharge-028 | 帮我搞点额度 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-055 | 我的额度用完了，想充值 300 元，请帮我办理 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-100 | 快给我充 200 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-071 | 帮我 top up 100 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-038 | 充额度 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-077 | 充 100 块到 account | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-012 | 充值 1000 元 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-074 | 帮我充值 100 CNY | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-023 | 帮我账户加点钱 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-063 | 我想多买点额度 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-001 | 我要充值 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-031 | 充值 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-002 | 充值 100 元 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-021 | 充点钱进去 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-036 | 加钱 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
| recharge-072 | 我要 recharge 200 | 复核后预览 | recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| recharge-080 | 麻烦帮我充一下值好吗 | 必要追问 | 无直接工具要求 | 请问要充值多少元？ | amount_yuan |
