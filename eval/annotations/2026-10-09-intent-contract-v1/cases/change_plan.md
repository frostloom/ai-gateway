# change_plan：100条线下标注草案

回答占位符需要后续fixture真值填充，非真实执行结果。所有条目尚非独立审核金标。

| ID | 原话 | 正确下一步（草案） | 允许工具 | 期望回答/事实模板 | 缺参或备注 |
|---|---|---|---|---|---|
| change_plan-019 | 我想升个级 | 必要追问 | list_plans | 您想办理哪项业务？请明确操作和目标。 | target_plan; 无上下文/单位/授权不能从原类别推定办理。 |
| change_plan-049 | 换档为 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-047 | 升极套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-036 | 套餐升级 #2 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan; 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| change_plan-030 | 换个额度多点的套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-052 | 你好，我想变更一下我的套餐，从标准档换成旗舰档，请帮我处理 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-005 | 套餐变更 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-039 | 套餐变更 #2 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan; 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| change_plan-095 | 直接改成旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-093 | 改套餐，快点 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-079 | 请问可以帮我改套餐吗 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-061 | 我额度老是不够 | 必要追问 | list_plans | 您想办理哪项业务？请明确操作和目标。 | target_plan; 无上下文/单位/授权不能从原类别推定办理。 |
| change_plan-092 | 马上升级到旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-063 | 想换个大的 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-027 | 我想换档位 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-058 | 我要升级我的套餐，换成额度更多的旗舰档 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-035 | 换档 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-032 | 换套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-077 | 帮我改 plan | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-070 | 标准太少了吧 | 必要追问 | list_plans | 您想办理哪项业务？请明确操作和目标。 | target_plan; 无上下文/单位/授权不能从原类别推定办理。 |
| change_plan-010 | 我要换档位 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-021 | 我这档不够用，换一个 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-023 | 帮我调到旗舰那个 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-083 | 劳驾帮我改下套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-090 | 给我换成旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-011 | 帮我改一下套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-082 | 可以帮忙变更套餐吗 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-009 | 更换套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-031 | 改套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-089 | 改套餐 #2 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan; 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| change_plan-022 | 想换个更大的套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-071 | 帮我 upgrade 套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-075 | upgrade 我的套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-045 | 套惨变更 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-007 | 我要升级套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-042 | 变更套惨 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-088 | 请问怎么改套餐 | 咨询解释 | list_plans | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| change_plan-038 | 换成旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-094 | 给我换成月度旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-008 | 把月度标准改成月度旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-060 | 我决定升级套餐了，麻烦帮我改到月度旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-097 | 给我改套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-069 | 我想升个档 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-003 | 把套餐改成月度旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-020 | 帮我换成贵的那个 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-051 | 我现在订的是月度标准，感觉额度不太够，想改成月度旗舰，麻烦帮我办理 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-085 | 方便的话帮我改成旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-014 | 把套餐升到旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-046 | 帮我该一下套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-065 | 怎么升级 | 咨询解释 | list_plans | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| change_plan-016 | 给我升级一下套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-002 | 变更套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-018 | 能不能换个档 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-078 | switch 一下套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-081 | 能不能帮我升级套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-048 | 我要换成其舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-012 | 套餐升级 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-043 | 帮我换套参 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-017 | 我这个套餐想换掉 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-087 | 麻烦您帮我变更套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-028 | 帮我改改套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-029 | 我要升级一下 | 必要追问 | list_plans | 您想办理哪项业务？请明确操作和目标。 | target_plan; 无上下文/单位/授权不能从原类别推定办理。 |
| change_plan-037 | 改档位 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-004 | 我要换套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-024 | 我要换个更好的 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-100 | 快给我改套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-076 | 我要 change 到 monthly 旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-006 | 帮我换到月度旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-074 | 帮我 switch 到旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-040 | 升档 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-044 | 我要换套惨 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-034 | 变更套餐 #2 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan; 生成器追加的#N是去重后缀，不是金额、订单号或真实业务参数。原话保留不变。 |
| change_plan-054 | 麻烦帮我改套餐，我要从月度标准换到月度旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-041 | 我要该套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-055 | 我现在的套餐额度经常不够用，想换个更大的，帮我改成旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-072 | 我要 change plan | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-091 | 立刻改套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-080 | 麻烦帮我换成月度旗舰好吗 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-086 | 能否帮我升级到旗舰档 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-033 | 升级套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-062 | 我这个档太小了 | 必要追问 | list_plans | 您想办理哪项业务？请明确操作和目标。 | target_plan; 无上下文/单位/授权不能从原类别推定办理。 |
| change_plan-053 | 我用的量变大了，想把套餐升级到更高档位，帮我改成月度旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-050 | 套餐便更 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-068 | 能不能换更高的 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-099 | 换成旗舰档 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-098 | 立即变更到旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-013 | 我要改成旗舰档 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-064 | 标准档不够用 | 必要追问 | list_plans | 您想办理哪项业务？请明确操作和目标。 | target_plan; 无上下文/单位/授权不能从原类别推定办理。 |
| change_plan-084 | 请问可以换档位吗 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-026 | 这个套餐太小了，换掉 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-056 | 我想变更套餐档位，从标准升到旗舰，请帮我操作 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-073 | 换成 pro plan | 必要追问 | list_plans | 您想办理哪项业务？请明确操作和目标。 | target_plan; 无上下文/单位/授权不能从原类别推定办理。 |
| change_plan-067 | 现在这个不够，怎么办 | 咨询解释 | list_plans | 说明用户询问的办理步骤/条件；办理写操作需明确参数并在预览后确认。 |  |
| change_plan-015 | 我想换个套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-066 | 我想提升额度 | 必要追问 | list_plans | 您想办理哪项业务？请明确操作和目标。 | target_plan; 无上下文/单位/授权不能从原类别推定办理。 |
| change_plan-025 | 给我升级到旗舰 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-059 | 请帮我把套餐从标准改成旗舰，我需要更多额度 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-096 | 升级，快点 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
| change_plan-057 | 帮我办理套餐变更，改到月度旗舰套餐 | 复核后预览 | change_plan | 将{operation}，{verified_amount_or_plan_and_effect}。请回复“确认”或“取消”。 |  |
| change_plan-001 | 我要改套餐 | 必要追问 | list_plans | 您想选择哪一个套餐？ | target_plan |
