# 完整窗口的纯评分计划

新增纯函数消费显式SHA锚定的manifest、summary、receipt、record和独立review，逐项绑定scope/window/pass/ordinal/arm/trace、回复与全部原要求。它从原Go JSON片段重建实际大写context结构，保留struct/map顺序、数字词形、Go字符串转义及null；不把review的verified=true或record自报摘要当作重算证明。

旧停止v2六窗334记录/1268要求全部重算通过；语义269通过/63失败/2无法判断，严格259通过/75失败，两条未运行行不造trace或score。确定性负例若独立语义正确，保留真实语义与分歧元数据，抑制正分，不将其伪记语义错误；原needs_review/Successfalse等不改。

独立审查发现P2：首窗已证明的quality-end后缀可在累计字段中改记为observed。root复现后，新增反例RED，首窗分项分别精确校验、后窗quality不得低于当前窗；此前窗口分项显式等待prior receipt链验证，不能据合计冒称已完整证明。修复后专项30项通过，完整Python230项/15原skip无失败，root再次运行30项通过。原沙箱临时HTTP替身bind拒绝保留，不计通过；获得所需本机权限后原完整回归通过。

当前只生成计划，publish_ready=false，没有CLI通信、评分发布、模型/JEV调用或实际claim。签名、冻结capture规则投影、实际trace验证、prior receipt链与一次发布锁仍由后续步骤完成；该纯计划不替代实际Langfuse评分交付。

源码、测试及日志SHA见[机器证据](evidence/full1220-window-score-plan-2026-10-29.json)，后续接口见[评分计划](superpowers/plans/2026-10-28-full1220-window-scores.md)。
