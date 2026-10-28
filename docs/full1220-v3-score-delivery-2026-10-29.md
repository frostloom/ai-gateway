# V3已采评测的评分与正文交付

已停止V3的482条实际trace、2813项观测、597轮正文和1442个独立评分全部读回核验。本次交付覆盖240完整对加1部分对；原3660对/7320臂/11772定义轮仍未完成，6838臂未尝试及11175未运行轮继续留在原分母。

新评分工具f01fa96通过272项Python回归（15原skip）及真实Ed25519正反测试；独立审查发现的“只核对现存intent子集误报完整”已保留RED并修复为原完整batch目录核验。原review当时暂缓全部通过分，后经独立发布资格审查及root逐项逆diff校验，仅释放318行的636个通过分；事实、判断、要求和原绑定完全不变。4个原负例即使语义正确仍抑制通过分，不冒充人工、盲审或新语义评级。

九窗分别一次POST168/166/168/168/167/168/168/167/102分，全部ACK。首次即时verify均返回未完整/exit2，原结果保留；后续只读verify全部exit0，没有重复发布。另一次35个GET保存标准化JSON，1442个唯一ID的值、dataType、subject、完整来源metadata及evaluation环境全部匹配。独立文件审计无确认P1/P2；其范围是保存的证据，不是独立供应商计费或网络发送次数证明。

服务端分类计数为：原状态414 needs_review/68 failed；语义318 passed/159 failed/1 not_assessable；严格318 passed/164 failed。另4个原负例的语义通过只保留在原review、分歧metadata及报告，不发布正分。未尝试22行不造trace或score。原Go Successfalse、cleanupfalse/observerfalse和第九窗停止状态不改。

以下仅比较ordinal1–240的480个完整臂；241两臂继续计入全部采集/技术失败分母，但不进入配对差值。样本为20 adv、100 balance、100 bills、20 cancel_subscription，仅pass1前缀，未随机抽样，不代表1220整体或三轮收益。

| 当前240完整对 | model | jev_selective |
|---|---:|---:|
| 独立非盲语义通过 | 159/240 | 163/240 |
| 严格组合通过 | 155/240 | 163/240 |
| 原生模型/JEV请求数 | 543/0 | 436/240 |
| 原账本配置费用，非账单 | ¥2.215610 | ¥1.800328及$0.012265 |
| 每个语义正确任务的费用向量 | ¥0.013934654 | ¥0.011044957及$0.000075245 |
| 模型输入cache hit/miss tokens | 886912/112661 | 714752/90584 |
| 输入token命中比例 | 88.729% | 88.752% |
| 任务server总时长P50/P95，秒 | 1.683/3.705 | 2.046/4.264 |
| 任务driver-step总时长P50/P95，秒 | 1.804/3.915 | 2.163/4.483 |
| conversation时长P50/P95，秒 | 1.850/4.051 | 2.205/4.525 |
| arm runner时长P50/P95，秒 | 1.976/4.608 | 2.324/4.653 |

费用按原冻结规则将全部归一化输入视作峰值cache miss、逐请求input/output组件向上取整；不能用该上界配置估计冒称供应商账单。USD与CNY不直接相加，不能据此声称总成本下降。当前缓存很高，不是全冷结果。所有分位数使用nearest-rank，任务时长按各任务实际轮次求和。

`turn.latency_ms`在冻结runner内被覆盖为整个driver.Step，包含State/Pending及owned wrapper开销，因此叫driver-step而非纯HTTP。server为response.telemetry.total_latency_ms之和；conversation在initial state验证后计时；arm runner在driver构建后包裹recordCollection及task.End。它们都不能冒称含全局fixture准备、删除、末尾flush和评分的用户E2E。

两臂knowledge off，观察到知识检索调用0；应取未取率及无效检索率因缺独立知识需求金标记N/A，不伪记0%。RAG/tool/resident/JEV预取仍为独立未运行补充，原100题不混入本表。

本轮评分/读回新增模型、JEV、财务claim、部署均0；实际历史仍1791/445，25claim永久¥38.888170+$0.308003，总额105+1。下一步是只读25claim证明和[新版本预算设计](full1220-after-v3-budget-design-2026-10-29.md)的离线验证，原scope不续跑。[机器证据](evidence/full1220-v3-score-delivery-2026-10-29.json)包含全部窗口和来源SHA。
