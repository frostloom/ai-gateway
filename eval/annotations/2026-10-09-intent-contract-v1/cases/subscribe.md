# subscribe：100条线下标注草案

回答占位符需要后续fixture真值填充，非真实执行结果。所有条目尚非独立审核金标。

| ID | 原话 | 正确下一步（草案） | 允许工具 | 期望回答/事实模板 | 缺参或备注 |
|---|---|---|---|---|---|
| subscribe-060 | 我决定订你们的套餐了，麻烦开通月度旗舰 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-023 | 我想订标准那个 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-009 | 我要订阅月度套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-051 | 我想订阅你们的月度标准套餐，麻烦帮我开通一下 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-026 | 帮我订个便宜的套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-008 | 帮我订套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-049 | 订阋月度套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-014 | 订阅套餐服务 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-012 | 帮我订月度标准 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-028 | 给我开个会员 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-038 | 买套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-063 | 怎么订更划算 | 咨询解释 | list_plans | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| subscribe-062 | 我想改成额度制 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-077 | 帮我 subscribe 旗舰套餐 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-003 | 帮我开通套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-084 | 请问怎么订阅套餐呢 | 咨询解释 | list_plans | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| subscribe-039 | 订购套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-048 | 我要定套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-024 | 给我开通下套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-091 | 立刻订阅套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-079 | 请问可以帮我订阅套餐吗 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-066 | 有没有包月的方式 | 咨询解释 | list_plans | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| subscribe-086 | 能否帮我开通套餐订阅 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-011 | 开通套餐订阅 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-020 | 给我整一个套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-097 | 给我订阅套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-092 | 马上订月度旗舰 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-042 | 订阅套惨 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-082 | 可以帮忙订购月度旗舰吗 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-058 | 我想开通月度套餐，这样额度刷新更划算 | 咨询解释 | list_plans | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| subscribe-088 | 请问可以订阅套餐吗 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-050 | 开通套惨 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-029 | 我要订阅那个旗舰的 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-006 | 开通月度标准套餐 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-094 | 给我开套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-090 | 给我开通月度标准 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-061 | 包月怎么开 | 咨询解释 | list_plans | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| subscribe-095 | 直接订月度标准 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-067 | 我想订个长期的 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-040 | 订旗舰 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-018 | 帮我订个会员 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-047 | 帮我订顶阅 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-036 | 订月度标准 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-030 | 帮我订个套餐试试 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-032 | 订套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-093 | 帮我订套餐，快点 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-033 | 开通套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-069 | 我想换个计费方式 | 必要追问 | list_plans | 您想办理哪项业务？请明确操作和目标。 | target_plan; 无上下文/单位/授权不能从原类别推定办理。 |
| subscribe-099 | 订月度标准 #2 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 | 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| subscribe-081 | 能不能帮我订个套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-065 | 按量太贵了 | 咨询解释 | list_plans | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| subscribe-100 | 快给我订套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-073 | 开通 pro plan | 必要追问 | list_plans | 您想办理哪项业务？请明确操作和目标。 | target_plan; 无上下文/单位/授权不能从原类别推定办理。 |
| subscribe-005 | 我要订购套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-035 | 订阅套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-083 | 劳驾帮我开通套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-015 | 我要包月 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-072 | 我要订阅 standard plan | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-089 | 订套餐 #2 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan; 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| subscribe-052 | 你好，我打算长期使用，想订阅一个月度套餐，请帮我办理 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-098 | 立即开通月度旗舰 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-054 | 麻烦帮我订购月度标准套餐，我想用额度制的方式 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-046 | 订阅月度标尊 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-041 | 我要订阋套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-057 | 帮我办理一下套餐订阅，我要订月度标准 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-027 | 我想包个月 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-096 | 开通，快点 | 必要追问 | list_plans | 您想办理哪项业务？请明确操作和目标。 | target_plan; 无上下文/单位/授权不能从原类别推定办理。 |
| subscribe-010 | 订购月度旗舰 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-053 | 我比较了一下，决定订阅月度旗舰套餐，麻烦帮我开通 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-007 | 订阅一个套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-044 | 我要订越 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-002 | 订阅月度标准 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-059 | 请帮我订阅套餐，选月度标准档位的 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-031 | 订阅 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-025 | 我要搞个套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-045 | 开通套参 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-076 | 我要开 plan | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-021 | 我要买套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-019 | 我想开通那个月度的 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-078 | subscribe 一下套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-075 | subscribe 月度标准 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-085 | 方便的话帮我订月度标准 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-071 | 帮我 subscribe monthly plan | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-013 | 我要开通订阅 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-055 | 我现在的按量付费太贵了，想改成订阅套餐，帮我订月度标准 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-004 | 订阅月度旗舰 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-022 | 帮我开个包月 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-017 | 我想订个包月的 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-080 | 麻烦帮我开通月度标准好吗 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-016 | 给我开个套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-068 | 怎么开通会员 | 咨询解释 | list_plans | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| subscribe-070 | 套餐怎么订 | 咨询解释 | list_plans | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| subscribe-064 | 我想长期用，选哪个 | 咨询解释 | list_plans | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| subscribe-037 | 开通订阅 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-043 | 帮我开同套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-074 | 帮我订个 monthly 套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-034 | 包月 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-056 | 我要订阅你们的套餐服务，选月度旗舰那个，请帮我开通 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| subscribe-001 | 我要订阅套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| subscribe-087 | 麻烦您帮我订月度旗舰 | 复核后预览 | subscribe_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
