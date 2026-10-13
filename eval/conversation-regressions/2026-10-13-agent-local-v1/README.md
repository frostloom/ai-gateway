# 本地真实客服链路回归证据

15条smoke通过真实agent、billing admin及隔离数据库，模型和JEV为本地脚本。events.jsonl是Go原始测试事件，summary.json仅提取事件中的计数；不包含逐轮原始数据库快照，不能据此独立审核完整回答。

结构通过15、语义待审15、完整任务成功率null。30次LLM HTTP请求均为本地替身，外部模型/JEV请求为0；未知token不填0。高置信/低置信行为是脚本机制验证，不是JEV真实性能。

manifest固定事件/摘要原始字节，并记录采集时代码hash。代码hash为历史来源，不要求未来工作区代码不变；测试独立固定manifest hash，防止同时改文件和清单。既有30条store证据与1220历史模型数据不覆盖。

事件文件保留PowerShell采集时的CRLF原始行尾，Git属性只为该文件允许CR行尾，避免格式检查要求改写证据。

详见[接入说明](../../../docs/customer-service-agent-integration-2026-10-13.md)。
