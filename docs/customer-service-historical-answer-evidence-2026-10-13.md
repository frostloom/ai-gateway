# 1220×3 历史首轮回答的证据资格复核

本次免费离线复核覆盖全部1220条原话、nojev/hint/fast三组3660个真实首轮结果。来源是2026-10-08 revised-full历史运行，fast不是当前V2。没有调用模型、JEV、数据库或网络，没有重跑评测，没有恢复当时的独立账户/账单/目录快照或完整多轮对话。

历史AI事后审核标签保留原样。它不是人工独立盲审金标，本次程序复核也不是第二次独立语义审读。程序新增的是可复算的来源校验、证据资格和数量提及筛查；独立事实核验数为0，数值真值正确率与完整任务成功率均为null。

|互斥证据状态|nojev|hint|fast|
|---|---:|---:|---:|
|proven-service-failure：真实HTTP/错误字段显示服务失败|97|107|97|
|existing-semantic-review-fail：保留既有AI语义失败|72|80|76|
|missing-historical-authority：既有不可核验，或数量提及缺独立历史来源|907|882|897|
|confirmation-only：既有AI回复通过且真实Pending，仅首轮待确认|55|55|55|
|existing-semantic-review-pass：既有AI语义通过、未命中数量筛查|89|96|95|
|总计|1220|1220|1220|

状态优先级为服务失败、既有语义失败、通过且Pending的确认预览、缺历史来源、既有语义通过。confirmation-only没有验证确认参数、真实扣款/订阅执行或多轮完成；数量和日期即使命中确认预览也没有事实资格。所有3660条的fact_evidence_eligibility均为not_evaluated，numeric_truth_verified/full_task_verified均为false。两个semantic状态是既有AI判断的归档视图，不能宣称已独立证明正确或错误。

必要追问仍可保持既有首轮动作/回复pass，缺来源不变成意图错误。服务失败保留在1220总样本内，未排除。既有首轮动作pass分别1100/1083/1089，回复pass分别247/258/260；这些都是历史AI语义标签，不是本次事实准确率。

|数量筛查|nojev|hint|fast|
|---|---:|---:|---:|
|回复中数量/日期提及候选（含失败和确认预览）|1002|989|994|
|既有reply pass但含数量提及且缺独立历史来源|158|162|165|

上表使用保守正则，覆盖货币、中文数量、token、比例、时间单位和日期。它不解析断言真假或归属；“近7天”、请求的充值金额、价格计量单位、用户举例与待确认预览也可能命中。因此158/162/165是复核候选清单，不是错误数、事实断言数或独立事实失败率。原pass不会被改写为fail；新增字段仅禁止把该pass当作数值事实证明。未命中也不证明没有事实声明，目录是否存在、政策限制等无数字声明同样缺独立来源。

逐条产物：[rows.json](../eval/reviews/2026-10-13-answer-evidence-v1/rows.json)包含原话、真实Reply、HTTP/Err、工具名、Pending、完整原AI动作/回复标签及理由、数量命中位置、互斥状态、原始预测规范JSON的SHA256。Reply来自冻结raw压缩结果，工具名/Pending均为可观察日志，不作为独立DB真值。每个arm的候选ID清单在[summary.json](../eval/reviews/2026-10-13-answer-evidence-v1/summary.json)。没有复制原始gzip和完整旧reviews；通过[sources.json](../eval/reviews/2026-10-13-answer-evidence-v1/sources.json)引用来源。

新审计脚本内固定旧archive-manifest、raw gzip、definitions、reviews和相关文件的SHA256，再检查gzip解压SHA、原实验gzip逐字节一致、1220唯一ID、三组完整覆盖、完整Sample与原定义一致、整个历史prediction/Reply与raw一致。verify从这些固定输入重算全部3660条和统计，再逐字节对比新产物，检查生成脚本SHA和目录文件集合。因此仅修改输出与其manifest，或修改旧来源与旧manifest，无法通过。相对路径禁止绝对路径、盘符与目录穿越，且检查解析后的路径仍在根内。SHA锚点依赖本脚本/版本库可信；能同时改写脚本锚点和所有证据的攻击不属于本地校验的防护边界。

生成命令只接受不存在的新目录，使用exclusive create，既有v1产物拒绝覆盖；后续规则变更应使用新版本目录。校验命令：

```powershell
$env:PYTHONIOENCODING='utf-8'
& D:/mnist_data/ancanda/python.exe scripts/audit-historical-answer-evidence.py verify
& D:/mnist_data/ancanda/python.exe -m unittest scripts.tests.test_historical_answer_evidence
```

已按TDD先确认新脚本缺失导致11条失败，再实现通过；随后加入“含金额的合理确认预览”反例，确认失败后修复状态优先级。测试覆盖数量/中文日期筛查、确认和追问边界、服务/语义失败保留、未知/重复ID、原话变更、目录穿越、固定来源拒绝协同manifest修改、真实3660条重算、输出Reply与manifest协同篡改以及拒绝覆盖。

初次沙箱Python全量107条出现6个failure记录、5个error记录，新审计11条通过。问题为既有实验launcher两条本地替身测试就绪失败、Windows占用清理错误、Git Bash的BaseNamedObjects访问拒绝及归档临时目录写入拒绝。最终在允许本地Git Bash/临时文件的授权环境复跑同一107条，全部通过（74.222秒），没有外部模型/JEV调用；没有扩大代码范围修改这些环境依赖。

这些结果没有新延迟、token、费用或当前模型质量成绩。历史提示/安全路径等组合差异仍然存在，不能把三组差异全部归因于JEV。完整任务/数值核验需要带时间及查询范围的真实独立历史状态或另行真实执行；本次未补造typed authority或使用模型回答证明自身。
