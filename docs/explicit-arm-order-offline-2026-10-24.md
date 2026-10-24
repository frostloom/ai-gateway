# 显式测评臂顺序离线交付

本小目标落实重复测评设计的第一单元：PairRunConfig 新增可选 `arm_order`。只允许 `[model,jev_selective]` 或 `[jev_selective,model]`，对该配置的每个案例采用指定顺序。默认 nil 不序列化，仍保留旧 suite 案例索引奇偶交替；单例默认仍模型臂先。未修改客服行为、定义/画像、运行合同、已授权 scope 或调用上限。

显式顺序参与 PairRunContract、模型预算和 journal 绑定，不能通过换顺序恢复已有 journal 或借用原预算。空非nil切片、重复、未知、单元素或多余元素在执行入口拒绝。omitempty 会将空切片与 nil 都省略，但非法空切片在写文件或调用依赖前验证，不能作为可执行配置绕过。JSON null 沿用默认 nil 语义。

RunAgentPair 在既有重建配置严格比较前验证顺序，再仅将验证过的 ArmOrder 复制到比较值；其他字段继续严格匹配。没有交换 routing 标签、报告行或案例定义，也不调整 JEV失败停止、预算或完成判定。

## 验证

首轮 TDD 已观察旧实现忽略显式顺序、显式摘要不变及空列表被执行的失败，最小实现后通过。真实 Handler 反向探针又复现适配层重建值缺 ArmOrder，导致调用前拒绝；最小适配修正后，原六案例在隔离 fixture、本地模型 transport 与 FakeJEV 下完成反向执行，实际 start 顺序逐条从 journal 核对。自动 needs_review 不作为真实质量通过。

覆盖旧序列化字段省略、旧默认多案例顺序、显式正反向 factory/journal、显式与旧模式/相反顺序禁止恢复、预算不混用；反向首臂清理失败只保留该失败臂。真实 Handler 的反向提供方503首次停止（1个替身模型请求/1次本地judge构造）、JEV失败首次停止（0模型请求/1次本地judge构造），均保留失败臂及未运行第二臂。无效配置不调用提供方或JEV工厂，不创建 journal/JEV guard。这里工厂次数不是供应商付费调用。

独立审查验证 nil 兼容、hash/预算/journal 绑定和适配层严格比较；建议的反向依赖失败及正反互换恢复测试已补。完整回归、旧文件完整性及隔离库核对结果与源码SHA见 `eval/paired-pilots/2026-10-24-explicit-arm-order-offline/validation.json`，中间失败保留，不以定向通过替代最终全量。

本轮没有新授权、真实claim、模型/JEV推理、评分写入或部署，实际累计仍60模型/15JEV，原永久预留不变。候选重复队列仍未实现、未获付费授权；当前新增的顺序能力不会自行启动重复测评。下一单元为独立重复scope、schedule/pair身份、永久预算链和受控执行入口，随后才可冻结最终计费材料并请求新精确范围批准。
