# 六批提案的可重复离线准备（2026-10-22）

`paired-eval -mode campaign-preflight` 将已审查的逐批案例和预算声明转换为完整配置、版本契约和分币种预留估算，替代上一轮临时生成脚本。它不读取密钥环境变量、不连接数据库、不打开预算账本，也不执行模型或 JEV 请求。输出始终 `allow_paid=false`、`ready_for_paid_run=false`、`provider_calls=0`、`authorized_spending=null`。

## 用法

在仓库根目录运行，先创建输出目录，再明确填写已审查的执行代码版本：

```sh
mkdir -p .tmp/campaign-preflight-20261022
SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk \
  go run ./cmd/paired-eval \
  -mode campaign-preflight \
  -definitions eval/task-definitions/2026-10-12-full-dev-v1 \
  -profiles eval/runtime-profiles/2026-10-13-six-profiles-v1 \
  -campaign-plan eval/paired-pilots/2026-10-22-campaign-preflight/plan.json \
  -model-base https://maas.qianwenaiapi.com/apps/anthropic \
  -model-name deepseek-v4.1-flash -protocol anthropic \
  -jev-model jev-1.13.0 \
  -revision REVIEWED_EXECUTION_REVISION \
  -out .tmp/campaign-preflight-20261022/proposal.json
```

`REVIEWED_EXECUTION_REVISION` 是必须替换的占位符。命令绑定调用者提供的版本，不会自行检查 Git HEAD、干净工作区或批准记录；版本变化后重新生成并审核。生成文件权限为0600，已有文件不会被覆盖。该命令没有 live 模式或付费开关。

## 输入与检查

[案例预算声明](../eval/paired-pilots/2026-10-22-campaign-preflight/plan.json)采用 `campaign-preflight-plan.v1`，包含 `campaign_limits` 和按顺序排列的 `batches`，每批指定案例 `key`、完整 `model_limits` 和 `max_jev_calls`。未知字段、尾随JSON、缺失或重复案例、冻结小样本之外的案例、无效限额、子预算不足、输出上限与配置不一致均被拒绝；失败不创建输出。

每批仅含一个案例，编号从 `batch-001` 开始。总批数、模型次数、JEV次数、人民币和美元限额各自检查，使用与实际 campaign 预算相同的纯内存预留规则；此步骤不会生成可执行的 lease 或可复用的模拟账本。模型限额按整数微元计算，JEV按每次美元微元声明预留，不换汇、不退回、不把未用次数分配给其他批次。

示例沿用[2026-10-21提案](paired-execution-proposal-2026-10-21.md)的声明：6案例、12臂、26轮；模型尝试上限24、JEV上限6；人民币估算预留811008微元（¥0.811008），美元预留16128微元（$0.016128），总限额分别为¥1和$0.02。它不是2026-10-22重新确认的价格、账户实际费率或消费授权。输出记录真实提示词、工具、冻结定义和profile哈希，并为每批生成 `PairRunContract`。

## 仍需完成的执行条件

账户实际费率与失败收费、精确渠道token包络，以及明确案例/次数/分币种预算授权继续待确认。输入字节限制不能替代token计数；声明预算也不是实际账单保证。该命令不构造执行驱动或observer、不实现自动批准、不把离线通过升级为真实模型质量或成本结论。将来实际执行须重新绑定所执行代码，创建独立初始账户、observer、全新campaign和日志，并逐批审核。

## 验证

新增测试先验证失败再实现。定向CLI/生成器/campaign测试与go vet通过；全项目Go回归373个顶层测试、17个测试包通过，零失败，3项显式跳过（真实JEV诊断和两项负载测量）。实际CLI生成的6案例/12臂/26轮及预留金额符合示例，HTTP哨兵调用为零；独立只读审查无实质问题。详细命令、源码和本地证据哈希见[核验回执](../eval/paired-pilots/2026-10-22-campaign-preflight/verification.json)。本次CLI烟测明确标记working-tree版本，不作为真实执行版本证据。
