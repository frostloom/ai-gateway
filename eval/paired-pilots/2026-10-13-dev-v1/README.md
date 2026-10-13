# 小规模配对准备归档

`plan.json` 是从冻结全量任务定义选择的 12 个完整开发任务，不是已运行结果或独立盲审金标。

SHA256：`c95fac67a08b515f4ae42c1213eb1f2725b1b120df8262ada7303342ce3b1232`。

完整规则、回答 assertions 和终态保留；准备时外部模型/JEV 请求为 0。调用草案为最多 24 次模型尝试、每次输出最多 128 token，`ready_for_paid_run=false`。实际价格和预算尚未核定，不能据此启动付费测评。

新 Go 归档测试独立固定文件 SHA 并对照冻结来源重建验证。请追加新版本，不覆盖此文件。

范围、接口和证据限制见[准备说明](../../../docs/customer-service-small-paired-preparation-2026-10-14.md)。
