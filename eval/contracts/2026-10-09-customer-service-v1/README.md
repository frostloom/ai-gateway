# 1220条版本化动作与离线评分契约

本目录是2026-10-09事后AI审核的追加契约层，共1220条。独立人工审核数量为0，独立历史账户fixture数量为0。它可用于语言/动作回归和后续任务标注准备，不能称盲测金标或历史完整任务验收集。

规则依据：[客服评测规范](../../../docs/customer-service-evaluation-contract-2026-10-09.md)。输入来源为冻结reviews.json及draft-cases.json；原始审核、历史输出、旧分数和分母均不修改。

## 文件及保留规则

- cases.json：新ID通过source_case_id关联历史ID，保留完整original。逐条列出允许动作/工具、必要槽位、事实要求、预览与确认规则、不可验证状态及来源。动作集合以逐条AI审核为准；槽位/参数仍明确标为待fixture和业务校验的规则草稿，不冒充已验证金标。
- evaluation-inputs.json：供新轮使用的text与original_text并存，历史原话不替换。新任务库须继续补初始状态、补参、确认/取消分支并另行冻结。
- normalization-proof.json：19条精确ID+原文白名单。生成器web/scripts/gen-intent-suite.mjs先按编号分配case，再洗牌；其738—739行对已用原句追加“ #N”。每条白名单记录同类、生成顺序在前的无后缀原句及源文件/生成器SHA。仅这些已证明生成后缀的文本可去尾缀；未知ID、原文变化及真实订单“#2”原样保留。此逻辑只在离线导出器内，无生产通用去数字规则。
- manifest.json：源文件、导出器与JSON产物SHA；动作类别数量；未实例化状态。校验既检查源又检查产物。规范、AI审核与槽位规则变化后必须创建新版本，不能覆盖冻结版本。
- archive-manifest.json：覆盖本目录全部文件（除自身），包括README与四个合成fixture；校验嵌套路径、SHA及完整文件集合，拒绝目录外路径。manifest要求该档案存在，删除档案不能绕过完整校验。新运行观察和分数应写入eval/reports等新目录，不写回冻结契约目录。
- fixtures/：独立设计的少量合成边界回归，数值仅为本地测试设定，绝不是1220条历史账户真值。

## 重现

在仓库根目录执行（全部标准Python库、完全离线）：

```powershell
& 'D:/mnist_data/ancanda/python.exe' scripts/customer-service-contract.py verify
& 'D:/mnist_data/ancanda/python.exe' -m unittest discover -s scripts/tests
# 新版本目录必须尚不存在；已有目录一律拒绝覆盖
& 'D:/mnist_data/ancanda/python.exe' scripts/customer-service-contract.py export --output eval/contracts/new-version
```

## 离线评分接口

```powershell
& 'D:/mnist_data/ancanda/python.exe' scripts/customer-service-contract.py score `
  --cases eval/contracts/2026-10-09-customer-service-v1/fixtures/cases.json `
  --observations eval/contracts/2026-10-09-customer-service-v1/fixtures/observations.json `
  --fixtures eval/contracts/2026-10-09-customer-service-v1/fixtures/truth.json `
  --output eval/reports/synthetic-offline-score-new.json
```

实际采集的Go IntentReport JSON或json.gz先导入，再离线评分：

```powershell
& 'D:/mnist_data/ancanda/python.exe' scripts/customer-service-contract.py observations `
  --cases eval/contracts/2026-10-09-customer-service-v1/cases.json `
  --report <新或旧IntentReport.json或.json.gz> `
  --output eval/reports/imported-observations-new.json
& 'D:/mnist_data/ancanda/python.exe' scripts/customer-service-contract.py score `
  --cases eval/contracts/2026-10-09-customer-service-v1/cases.json `
  --observations eval/reports/imported-observations-new.json `
  --output eval/reports/imported-scores-new.json
```

导入器以契约source_case_id与Sample.id配对；样本文本只能是原文或该case的规范化text。重复、陌生、非法ID或文本不匹配均拒绝。缺样本不制造观察记录，score仍保留全部case分母。优先使用RawBody原始trace、pending字段及reply，否则读取报告Trace；HTTPCode、Err、HTTPError及原始RawBody保留。每条观察附采集报告SHA和路径。

只有实际trace的phase=preview、outcome=pending、对应pending=true/tool一致，才能生成待确认观察；参数仅取服务端resolved_args，不从模型请求args推断。缺resolved_args时pending仅保留tool，参数仍不可验证。旧报告只有AllTools/Pending标志时，保留为原始诊断字段，不补造trace、phase或参数。导入器assertions始终为空，不从模型回复“已完成”或历史AI pass生成语义/数值/状态断言；必要追问缺可信语义证据仍不可验证。需要数值/完整任务评分时，另由可信测量器补独立fixture及assertions，再以新文件保存。

score输出也用独占创建，重复执行须选择新输出路径。输入均为JSON数组，ID必须匹配且唯一。未返回的预定case留在分母；陌生ID和重复ID拒绝。

observation字段：id、http_code、err、trace、pending、reply、assertions。trace为**首轮**工具轨迹，每项包含tool及phase；写工具的phase必须明确为preview，否则按提前执行失败处理。pending为null或含tool/params的对象，不接受仅凭boolean声称正确预览。最终多轮状态通过assertions记录，首轮trace不混入确认后的合法执行。

assertions.action包含kind、reply_span及追问slot；reply_span必须确实出现在reply中，slot必须属于缺失槽位。允许合法只读后聚焦追问；没有工具不自动证明追问/拒绝/闲聊。首工具不是主评分项。语义动作assertion由可信离线人工/审核器/extractor提供，存在真实span仅证明证据位置，不自动证明语义准确；不能从模型自述“已完成”构造assertions。没有assertion的无工具回复保持unverifiable。

数值/回答/完整任务必须另给独立fixture：id、independent=true、initial_state_valid=true、source（synthetic_regression或frozen_pre_run_fixture）、evidence_ref、expected_answer、expected_state，必要时expected_pending。测量assertions提供answer、state、terminal和evidence_ref。expected_state必须包含forbidden_side_effects=0才可能full_task pass；写申请还必须有最终pending=false及对应测量，证明待确认已清除。所有要求的回答/状态键均须出现并精确匹配，错误键判fail，缺键保持unverifiable。完整任务还要求terminal=true；预览或正确追问本身不算终态。expected_pending核验首轮预览工具及完整参数对象，未提供真值时仅可观察预览动作，参数正确性不可验证。

如果fixture声明expected_pending或case要求preview_required_before_execution，完整任务还必须preview_parameters=pass。缺预览参数、缺预览观察或缺独立预览真值均保持unverifiable，即使最终回答、状态和terminal断言正确也不能绕过必要预览核验。

expected_answer必须覆盖该case全部必要回答事实，expected_state必须覆盖目标及全部禁止副作用；fixture要在运行前独立冻结，并由可信测量器记录真实工具/数据库/账本证据。接口无法自行证明外部文件的独立性及断言提取质量，source/evidence_ref是假定可信的调用者契约，不能将一个随意声明independent的历史回复文件视为真值。后续真实任务库需在运行前审定这些完整断言。

输出五维互不替代：next_action、answer、numeric_truth、preview_parameters、full_task，值为pass/fail/unverifiable/service_failure。HTTP非200或err优先记service_failure，全部留在计划case分母，单列且不改称意图错误。HTTP证据缺失不可判正常成功。初始化失败需在调用者的独立初始化清单单列，不能在本工具中伪装服务失败；这里报告的是预定case数，不声称已有有效初始状态的任务成功率。

评分器不输出“通过/可判定项”冒充全量成功率。1220条缺少独立真值，因此数值准确率与完整任务完成率仍不可计算。本次未请求模型/JEV/在线服务，也未重跑1220。
