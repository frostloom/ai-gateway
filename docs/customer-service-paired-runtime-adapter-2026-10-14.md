# 配对运行适配器与离线预检查

2026-10-14。本阶段接入真实客服 handler、工具、确认状态机和独立状态查询，验证仅使用本机模型 HTTP 与脚本 JEV。没有外部模型/JEV推理，没有读取 `.env`，没有重跑1220条或新增真实V1/V2成绩。

## 执行边界

- `PrepareAgentPairConfig` 用显式模型、协议和公开端点计算实际提示词/工具/端点摘要；HTTP transport 禁止请求，不建数据库。
- `OpenPairedFixtureEnvironment` 默认禁止创建；启用后只接受显式本机环回 MySQL 管理连接，不接受已有数据库名。创建随机独立库、私有 miniredis，只删除本程序成功创建的库。
- `RunAgentPair` 默认禁止执行；显式启用后仍要求隔离环境、冻结定义、绑定运行配置的模型预算和明确有上限的 JEV 工厂。两臂共享短提示词、工具与128 token上限，每臂独立账户。
- `recorded_run` 额外要求付费开关、渠道费率已核定声明、显式密钥、JEV版本及调用许可。声明不是供应商账单证明；可信调用方负责授权和正确费率，适配器不是任意 Go 代码的网络沙箱。
- `local_stub` 模型只接受字面环回IP；JEV替身由可信显式工厂提供。私有 miniredis 验证业务状态，不证明生产 Redis 部署和并发能力。

所有模型上游尝试都经过预算 transport，发送前持久化预留，限制次数、请求大小、输出和按声明费率估算的金额；usage未知或越界停止后续模型尝试。重启绑定配置、原预算及记录，不自动重跑未完成请求。JEV发送前持久化次数，限制版本/次数，并将路径、契约、上限、累计次数和停止状态绑定主运行的预算快照；JEV日志丢失或截断不能恢复零计数继续请求。JEV失败可能按客服既有逻辑回退模型，但模型仍受预算保护。

JEV限制是次数限制，不是供应商人民币账单限制。清理错误向调用者返回，不能吞掉锁文件删除失败。隔离环境由调用方 `Close`，清理失败应保留失败记录和诊断，不得删除任意现有库。

## 只读配置准备

CLI提供 `prepare`、`preflight`、`report`，没有在线执行或凭据参数。使用当前已审核提交的完整SHA替换下面的版本文字；输出只能写入新文件。

```powershell
go run ./cmd/paired-eval -mode preflight `
  -definitions eval/task-definitions/2026-10-12-full-dev-v1 `
  -profiles eval/runtime-profiles/2026-10-13-six-profiles-v1 `
  -model-base https://maas.qianwenaiapi.com/apps/anthropic `
  -model-name deepseek-v4.1-flash -protocol anthropic `
  -revision '替换为审核提交SHA' -jev-model jev-1.13.0 `
  -out .paired-preflight.json
```

版本文字需替换为审核过的完整提交SHA。预检查不查询账号、不连接数据库、不调用模型/JEV，固定输出 `ready_for_paid_run:false`、`provider_calls:0`、`AllowPaid:false`。真实运行仅提供显式 Go API，不能把这个配置文件当成已批准付费运行。

## 验证范围

真实 handler + 隔离 MySQL/miniredis + 本机 HTTP/JEV 替身验证余额查询与充值确认两任务、各两臂。每任务两臂共2次本机模型 HTTP；确认充值只写一次，两臂账户独立，完成记录重复运行不再请求，改端点或截断 JEV 日志恢复被拒绝。实际响应仍标记待语义审核，机制测试不是模型成绩或JEV节省比例。

工程独立审查发现JEV日志截断后计数恢复漏洞：修复前两任务回归失败，绑定预算快照后通过；关闭错误吞掉问题由代码审查发现并修复，不声称经过同样的失败复现。见[标准复核](customer-service-pilot-standard-audit-2026-10-14.md)、[价格依据](customer-service-pilot-pricing-2026-10-14.md)与[面试记录](interview/CHANGELOG.md)。
