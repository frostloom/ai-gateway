# AI 客服 Pi DeepSeek 评测入口（2026-10-22）

本次适配的是 AI 客服的评测准备与受控执行入口。Langfuse 继续作为已有的对话、工具、用量与评分观测服务；未修改 Langfuse 本身或正在运行的客服模型配置。

## 现在支持的行为

`campaign-preflight-plan.v2` 显式绑定以下公开目标，旧 v1 计划及授权保留历史行为：

| 字段 | 当前评测值 |
| --- | --- |
| 模型地址 | `https://api.deepseek.com/v1` |
| 模型 | `deepseek-flash` |
| 协议 | `openai` |
| JEV | `jev-1.13.0` |

`paired-eval` 的 campaign-preflight 校验命令行目标与计划一致，再离线生成配置与预算演算。`paired-live` 和首批执行库现在也理解独立的 v2 授权：必须使用 v2 schema、显式 `model_base`、与新计划一致的 SHA、模型和协议。旧千问授权、旧收费证据不能混用。

仍保留六案例、24 次模型尝试、6 次 JEV、¥1 / $0.02 上限，以及每次输出 128 token、请求 16384 字节、输入声明上限 16384 token。首批结束即 checkpoint，不自动继续；上游失败停止，不自动重试。独立 fixture、durable claim、未修改 VCS 构建、实际用量及观测要求继续生效。

新版本支持绑定新模型，不会从本次配置迁移自动生成授权文件或将 `RatesVerified` 设为 true。本次没有创建新的付费授权，也没有执行真实模型/JEV推理。

## 已验证的结果

新档案位于 `eval/paired-pilots/2026-10-22-pi-customer-preflight/`：

- `plan.json`：独立 v2 计划，不改写旧冻结文件。
- `proposal.json`：绑定代码提交 `2e98b1e7201ef670cd942a1a7c66182f6fd77987`，六案例、12 臂、26 用户轮次，provider calls=0，allow_paid=false。
- `settings-check.json`：显式配置及本机 Langfuse 健康/认证检查通过；只验证配置，`runtime_binding_valid=false`，不代表已验证实际执行所需的全部 runtime 绑定。
- `manifest.json`：上述文件与相关入口源码的 SHA、验证范围与零推理声明。
- `evaluation.env.example`：只为评测固定 JEV 版本的无密钥覆盖文件。

沿用旧计划的 2/8 元每百万 token 声明值，仅用于离线演算；预留模型金额 811008 微元、JEV 16128 微美元是声明的预算占用，不是当前渠道的已核实费率或实际花费。准备结果明确保持所有收费证据状态为 false、ready_for_paid_run=false，不重复执行余额或供应商价格核查。

Go 全项目输出中 45 个包完成，18 个含测试的包全部通过。Python 脚本共 162 项，147 项通过、15 项条件跳过。新增测试先失败再实现，覆盖新旧目标交叉绑定、错误配置、未审批输入、私密环境隔离，以及 OpenAI 本地替身下真实 handler/隔离 fixture/余额工具回填/OTLP trace/首批报告与失败停止。独立代码审查无发现。本地替身结果不算真实模型质量结论。

## 重复运行本机配置检查

在新输出路径运行：

```sh
python3 scripts/langfuse-eval.py check \
  --campaign-plan eval/paired-pilots/2026-10-22-pi-customer-preflight/plan.json \
  --env-file .env \
  --env-file worktrees/langfuse-observability/deploy/langfuse/.env.agent \
  --env-file eval/paired-pilots/2026-10-22-pi-customer-preflight/evaluation.env.example \
  --out .tmp/pi-customer-settings-next.json
```

新入口只读取列出的配置文件，不继承进程环境里的凭据或模型设置。`--campaign-plan` 与旧 1220 案例的 `--preflight` 互斥。只读取本机 Langfuse 的 health 和 scores 认证接口，不向模型发请求或写评分。

本机原客服配置没有设置 `JEV_MODEL`，运行服务使用已有默认值。第一次新检查因此正确拒绝 `pinned_jev_model`；加入公开的评测覆盖文件后通过。该文件仅为评测 pin 到 `jev-1.13.0`，没有应用到运行服务，不把“评测配置已通过”写成“生产 JEV 已升级/已固定版本”。

下一阶段才是绑定当前 Pi 目标的受控真实首批执行。届时仍需完整执行条件，当前准备档案和旧授权都不能单独证明付费执行已就绪。
