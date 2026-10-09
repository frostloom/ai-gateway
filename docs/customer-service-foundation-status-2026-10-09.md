# 客服评测基础修复交付说明

日期：2026-10-09。范围：用户授权的前四项。没有运行真实模型/JEV或重新执行1220条在线评测；历史档案和旧分数不变。

## 1. 标注与评分

[1220条契约](../eval/contracts/2026-10-09-customer-service-v1/README.md)保存原文、审核动作、允许工具、缺失槽位、回答要求及确认规则。19个有生成器来源证明的去重后缀仅在评测输入中规范化；生产端不普遍删除“#数字”，真实订单号保留。

标注仍为事后AI审核，独立人工金标和1220条独立账户fixture均为0。槽位参数草稿和待核验状态显式保留。规范定稿不等于已获得完整任务真值。

新Python评分器分别输出next_action、answer、numeric_truth、preview_parameters、full_task。缺证据不可核验，服务失败留在分母；需要预览的任务必须核验预览参数，才能判完整成功。历史JSON/gz或新Go采集结果可先用observations命令转换，再用score评分；不会从模型自述生成可信断言。命令详见契约README。

Go评测入口新增 `-contract eval/contracts/2026-10-09-customer-service-v1/cases.json`，实际加载规范化输入并保留原文/源ID/契约ID，报告SHA来自同次读取的cases内容。默认旧 `-dir` 仍为历史工具诊断；契约fixture未实例化时清空声明fixtureSHA，不能伪装主实验数据。本轮只测试加载，没有执行在线评测命令。

## 2. 共享客服修复

- 套餐名称“旗舰档/旗舰套餐/标准档”等只在目录唯一匹配时生成预览；多个可能目标仍追问。
- 无待确认状态时，模型不能直接邀请确认。检测到明显不一致时最多一次模型纠正，额外调用和usage照实记录；仍不一致返回未执行说明，不陷入重复循环。
- 用户已给金额时纠正重复金额追问；确实缺金额的追问保留。此为有限的一致性修复，不能证明所有语言表述已被模型正确理解。
- 已有待确认操作不被另一写请求静默替换；必须先明确确认/取消。
- 退入余额、提现、credit单位不明先澄清；纯办理方法咨询不生成写预览，咨询并明确申请的情况保留合法预览。
- 退订同时询问余额怎么退时，预览保留退款说明；模型仅发退订工具而没有正文也不会漏掉这项咨询。
- 工具说明明确现金金额与token不同、消费统计仅提供扣费、账单最多10笔；不把局部结果包装成全月或从零扣费推出零token。

共享提示词从1564字符缩短至770字符，默认1–2句，必要目录/列表除外，以保留必需事实为约束，不通过强行裁剪文本来压缩回复；字符缩减不是实测token、成本或时延收益。OpenAI输出预算现在实际发送，Anthropic保留既有预算与重试机制。

## 3. 运行证据

新intent schema3保留HTTP原文、完整/原始trace、模型提出及服务端解析参数、工具返回、真实预览、待确认工具、session、HTTP错误及原始telemetry。API错误路径也返回配置和耗时。

每轮记录模型/JEV实际调用、token、usage缺失及阶段耗时；供应商原始usage保留缓存/推理字段的缺失与显式零区别。新taskbench.v3保存初始/最终账务与订阅状态、逐轮快照、确认取消及幂等证据。初始化失败独列；已有有效fixture的HTTP失败仍算未完成，不藏进成功子集。无价格/usage时费用不可评估，不填0。

写操作回复/预览与权威金额明显矛盾会失败；固定任务集仍是开发回归，尚未成为独立盲测集，任意自然语言回答的所有事实也没有全面自动核验。

## 4. 对照模式

|AGENT_ROUTING_MODE|行为|用途|
|---|---|---|
|model|不调用JEV，由模型选工具和回答|主V1|
|jev_selective|仅高置信、首轮独立无参数四读直达；其他交同一模型|主V2，默认模式|
|jev_hint|JEV意图提示交给模型，无主实验外加安全守门|辅助提示消融|
|legacy|保留过去提示/安全拦截/写守门组合|历史诊断，不能作为纯V2|

四读为当前现金余额、默认近7天扣费、全部套餐目录、本人当前订阅。intent/shape≥0.90、legit≥0.85、risk<0.5、risk confidence≥0.8；复杂、缺参数、上下文不足、schema无效、JEV故障均回退。主V2不额外注入提示，不做JEV风险短路拦截或第二次写守门；两组使用相同执行安全、预览与确认。显式确认/取消由共享状态机处理，不调用模型/JEV。

主比较校验真实模型、协议、去除认证后的网关身份、prompt/tool/runtime契约哈希、输出预算、阈值、实际评测契约/数据/fixture哈希、并发、样本及路由模式。配置或证据缺失拒绝主归因；历史schema2只保留工具路径诊断，首工具命中不叫任务完成率。

根Docker Compose仅包含基础设施，无agent服务配置；路由模式由cmd/agent读取，不新增无效部署映射。无JEV key时明确记录模型回退，不能据此声称验证JEV收益。

## 本地验证

使用假模型/假JEV、本机HTTP测试服务和已有Docker内的独立MySQL/Redis测试库。按TDD复现缺陷后修复；三项独立审查问题和咨询加明确申请的回归已处理。最终五个Go包的完整测试通过，Python离线测试52/52通过，旧档案及新契约校验通过。验证命令：

```powershell
go test ./internal/agent ./internal/taskbench ./cmd/agent ./cmd/eval ./cmd/task-eval -count=1
python -m unittest discover -s scripts/tests
python scripts/customer-service-contract.py verify
python scripts/archive-intent-contract.py verify
python scripts/summarize-offline-intent-audit.py verify
```

这些验证证明本地实现和证据完整性，不是最新模型的在线成绩。下一阶段才是补独立任务fixture、预冻结运行配置、先验证边界小集再决定是否完整在线评测。
