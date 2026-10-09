# cancel_subscription：100条线下标注草案

回答占位符需要后续fixture真值填充，非真实执行结果。所有条目尚非独立审核金标。

| ID | 原话 | 正确下一步（草案） | 允许工具 | 期望回答/事实模板 | 缺参或备注 |
|---|---|---|---|---|---|
| cancel_subscription-005 | 我要取消套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-023 | 我不包月了 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-006 | 帮我退订 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-014 | 我要取消自动续费 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-043 | 退订套惨 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-053 | 因为我换了别的方案，想取消现在这个套餐订阅，帮我办理一下 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-096 | 取消，快点 | 必要追问 | 无直接工具要求 | 您想办理哪项业务？请明确操作和目标。 | operation_target; 无上下文/单位/授权不能从原类别推定办理。 |
| cancel_subscription-029 | 帮我关掉套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-011 | 帮我取消订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-072 | 我要 unsubscribe | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-033 | 退套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-040 | 不续了 #2 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 | 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| cancel_subscription-044 | 帮我取消订越 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-018 | 帮我停掉订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-016 | 把这套餐停了吧 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-082 | 可以帮忙取消订阅吗 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-094 | 给我停掉订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-081 | 能不能帮我退掉套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-077 | 帮我 stop 订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-048 | 我要退顶 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-098 | 立即取消订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-028 | 不订了，取消 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-075 | cancel 掉套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-004 | 取消我的订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-002 | 取消订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-055 | 我打算停止使用你们的服务，请帮我取消套餐订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-067 | 不想自动扣款 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-068 | 把续费关掉 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-080 | 麻烦帮我取消订阅好吗 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-035 | 退订套餐 #2 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 | 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| cancel_subscription-034 | 取消套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-013 | 取消套餐订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-049 | 停掉套惨 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-042 | 取消订阋 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-019 | 我不想续费了 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-093 | 退订，快点 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-071 | 帮我 cancel subscription | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-010 | 我要停止订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-066 | 我要停止付费 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-059 | 请帮我退订套餐，我不想继续扣费了 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-087 | 麻烦您帮我取消订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-057 | 帮我办理退订，取消月度旗舰套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-050 | 取消订越 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-090 | 给我取消订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-089 | 退订 #2 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 | 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| cancel_subscription-007 | 取消月度标准订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-026 | 我不想再订了 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-073 | 取消我的 plan | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-032 | 取消订阅 #2 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 | 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| cancel_subscription-017 | 不续了 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-097 | 给我退订套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-076 | 我要 unsubscribe 订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-100 | 快给我退订 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-092 | 马上取消套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-003 | 退订套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-088 | 请问怎么退订 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| cancel_subscription-069 | 我不需要这个了 | 必要追问 | 无直接工具要求 | 您想办理哪项业务？请明确操作和目标。 | operation_target; 无上下文/单位/授权不能从原类别推定办理。 |
| cancel_subscription-070 | 怎么停止订阅 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| cancel_subscription-063 | 下个月不要扣了 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-039 | 停套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-038 | 退订服务 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-054 | 麻烦帮我退订，我不需要这个套餐了 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-001 | 我要退订 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-099 | 停掉套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-024 | 退了吧 | 必要追问 | 无直接工具要求 | 您想办理哪项业务？请明确操作和目标。 | operation_target; 无上下文/单位/授权不能从原类别推定办理。 |
| cancel_subscription-030 | 我不继续了 | 必要追问 | 无直接工具要求 | 您想办理哪项业务？请明确操作和目标。 | operation_target; 无上下文/单位/授权不能从原类别推定办理。 |
| cancel_subscription-046 | 退订顶阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-058 | 我不再需要这个订阅了，麻烦帮我取消 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-085 | 方便的话帮我取消订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-031 | 退订 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-041 | 我要退定 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-086 | 能否帮我退订当前套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-036 | 停订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-009 | 取消订阅服务 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-027 | 把包月取消 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-025 | 停了这订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-051 | 我不想继续使用这个套餐了，麻烦帮我取消订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-078 | 取消 subscription | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-052 | 你好，我决定不再续费，请帮我退订当前的月度标准套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-012 | 退订当前套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-091 | 立刻退订 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-045 | 我要消订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-047 | 取消订阋服务 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-037 | 取消自动续费 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-062 | 怎么停掉 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| cancel_subscription-021 | 这个套餐我不想要了 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-061 | 我不想被扣费了 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-074 | 帮我退订 plan | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-015 | 我不想订了 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-095 | 直接取消订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-008 | 退订月度旗舰 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-084 | 请问可以取消自动续费吗 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-079 | 请问可以帮我退订吗 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-020 | 取消掉吧 | 必要追问 | 无直接工具要求 | 您想办理哪项业务？请明确操作和目标。 | operation_target; 无上下文/单位/授权不能从原类别推定办理。 |
| cancel_subscription-064 | 我不想续了 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-022 | 帮我把订阅关了 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-065 | 怎么取消 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| cancel_subscription-060 | 我决定停掉这个订阅，麻烦帮我取消月度标准 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-083 | 劳驾帮我退订套餐 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| cancel_subscription-056 | 我想取消自动续费，退订现在的订阅 | 复核后预览 | cancel_subscription | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
