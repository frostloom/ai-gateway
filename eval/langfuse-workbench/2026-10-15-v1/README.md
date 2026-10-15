# 本机 Langfuse 工作台证据

开始日期 2026-10-15，验收日期 2026-10-16。零外部模型/JEV 调用。

- **最终计划**：plan-v3.json；来源为冻结开发任务、四份旧 local_stub 报告和 trace-scope.json。1220 项是定义，不是新执行结果或独立金标。
- **最终发布/核验**：publish-4.jsonl、idempotence-receipt.jsonl、readback.json。重复发布零新增 POST。
- **浏览器**：browser-validation.json 为最终验收；browser-validation-first.json 保留首次脚本的误判，不作为通过依据。
- **提示词关联**：prompt-link-probe.json、prompt-link-readback.json、prompt-link-probe.go.txt。3 个纯观测节点，不执行推理，独立 release。
- **过程证据**：plan.json.gz、plan-v2.json.gz 为字节可还原的旧计划；publish-1 主动中断，publish-2 接口拒绝，publish-3 只做配置读回。错误看板的快照及受控移除回执也保留，不能将其当最终查询通过。
- **验证与摘要**：validation.json、manifest.json。所有金额未知仍未知；原有 56 分数和 8 任务的成功状态仍 unknown。

完整说明：../../../docs/langfuse-workbench-2026-10-16.md。
