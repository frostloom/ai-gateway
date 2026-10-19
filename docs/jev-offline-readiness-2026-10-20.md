# JEV 分流与效率统计离线核查

## 目标 1：本地执行门控

新增确定性审计测试直接调用 `fastPathReason`，不连接 JEV、模型、数据库或 HTTP。逐项核对意图/形态 0.90、合法性 0.85、风险置信度 0.80 的等号与相邻浮点值，以及风险分数 0.50 的排除边界。NaN、Infinity、负风险、不可用结果和无效 schema 均回退；待确认退款、多轮上下文和充值意图不得绕过执行边界。四个既有读工具白名单保持不变。

另运行既有 fast-path、schema、路由及写操作边界集成回归：

```powershell
$env:AGENT_LIVE_JEV='0'
go test ./internal/agent -run 'TestSelectiveGateExactBoundaries|TestJevFastPath|TestFastAnswers|TestRouting|TestSelectiveRouting|TestSelectiveWrite' -count=1 -json
```

退出 0，逐测试结果见 [门控摘要](../eval/jev-offline-readiness/2026-10-20/gate-summary.json)，完整压缩日志同目录。此轮只补现有行为的测试，未调整阈值或产品策略。既有集成测试使用模拟推理及专用测试数据库。

本地执行门控正确不等于 JEV 意图准确、置信度已校准，不能把这些测试通过率当真实用户准确率或 1220 条评测成绩。
