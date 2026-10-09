# refund：100条线下标注草案

回答占位符需要后续fixture真值填充，非真实执行结果。所有条目尚非独立审核金标。

| ID | 原话 | 正确下一步（草案） | 允许工具 | 期望回答/事实模板 | 缺参或备注 |
|---|---|---|---|---|---|
| refund-038 | 退额度 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-040 | 来退款 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-039 | 退 200 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-087 | 麻烦您帮我退 500 元 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-032 | 退钱 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-098 | 立即退款 100 元 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-003 | 退款 100 元 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-069 | 我反悔了 | 必要追问 | 无直接工具要求 | 您想办理哪项业务？请明确操作和目标。 | amount_yuan_or_owned_paid_order; 无上下文/单位/授权不能从原类别推定办理。 |
| refund-090 | 给我退 200 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-018 | 能退吗，我不用了 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| refund-034 | 申请退款 #2 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order; 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| refund-089 | 退 100 #2 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 | 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| refund-047 | 退环 100 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-046 | 退款 200 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-022 | 我要把钱拿回来 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-004 | 查询退款入口 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| refund-041 | 我要退宽 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-042 | 退款 100 元 #2 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 | 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| refund-083 | 劳驾帮我退 100 块 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-026 | 我先退 200 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-011 | 给这个账户退款 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-097 | 给我退 50 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-021 | 不玩了，退款 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-074 | 我要 refund 200 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-078 | 帮我 refund 一下 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-085 | 方便的话帮我退 300 元 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-054 | 麻烦帮我退款 100 元，因为充多了用不完 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-019 | 我想退一百块 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-075 | refund 我的账户 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-059 | 麻烦把充值的钱退给我，先退 50 元试试 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-056 | 我要申请退款 1000 元，请帮我走一下流程 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-079 | 请问可以帮我退款 100 元吗 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-091 | 立刻退款 100 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-064 | 不用了，怎么处理 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| refund-077 | 我要 withdraw 100 | 必要追问 | 无直接工具要求 | 您想办理哪项业务？请明确操作和目标。 | amount_yuan_or_owned_paid_order; 无上下文/单位/授权不能从原类别推定办理。 |
| refund-031 | 退款 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-014 | 我要退充值 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-013 | 帮我申请退款 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-035 | 退 50 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-088 | 请问退款怎么操作 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| refund-024 | 给我退钱 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-037 | 退款 100 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-048 | 把钱退给我 #2 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order; 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| refund-005 | 帮我办理退款 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-058 | 我需要退款，请帮我处理 200 元的退款申请 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-033 | 退 100 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-099 | 退 100 元 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-007 | 退还 50 元 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-061 | 我不想用了，钱能退吗 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| refund-063 | 能退吗 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| refund-012 | 退款 100 元到余额 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-052 | 你好，我想申请退款，金额是 200 元，请帮我处理一下 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-096 | 退钱，快点 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-076 | 帮我退款 100 CNY | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-045 | 退 50 块 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-071 | 帮我 refund 100 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-068 | 不续了，余额怎么退 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| refund-084 | 请问可以退款吗 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| refund-067 | 能不能退回来 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-050 | 退宽 100 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-006 | 我要退钱 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-073 | 退 100 到 balance | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-070 | 想把钱要回来 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-001 | 我要退款 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-095 | 直接退 100 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-065 | 这钱还能拿回来吗 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| refund-027 | 充多了，退点 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-049 | 退k 100 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-036 | 退余额 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-028 | 帮我把多的退掉 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-010 | 我要退 500 元 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-025 | 退 50 回来 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-029 | 退个三五百 | 必要追问 | 无直接工具要求 | 您想办理哪项业务？请明确操作和目标。 | amount_yuan_or_owned_paid_order; 无上下文/单位/授权不能从原类别推定办理。 |
| refund-015 | 我不想用了，退钱 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-082 | 可以帮忙申请退款吗 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-044 | 我要退矿 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-030 | 我不想充了，退钱 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-043 | 帮我退钱 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-009 | 退款操作 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-100 | 快给我退 200 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-055 | 我需要把之前多充的钱退出来，申请退款 300 元 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-017 | 我要退掉，钱还我 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-062 | 充多了怎么办 | 咨询解释 | 无直接工具要求 | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| refund-057 | 帮我退 100 块钱吧，我不打算用了 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-053 | 我不打算继续使用你们的服务了，想把账户里的钱退回来，先退 500 元 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-093 | 退款 100，快点 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-081 | 能不能帮我退 200 元 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-080 | 麻烦帮我办理退款好吗 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-072 | 我要申请 refund | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-002 | 申请退款 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-086 | 能否帮我办理退款 100 元 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-020 | 帮我把钱退回来 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-060 | 我想退回 500 元，不想继续充值了 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-023 | 帮我退点钱 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-008 | 帮我退 200 块 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-051 | 我之前充值了一笔钱，现在用不上了，麻烦帮我办理退款 100 元 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-092 | 马上退 500 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-094 | 给我退 300 | 复核后预览 | refund_recharge | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| refund-016 | 把钱退给我 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
| refund-066 | 我想撤回充值 | 必要追问 | 无直接工具要求 | 请提供退款金额或本人已支付充值订单号。 | amount_yuan_or_owned_paid_order |
