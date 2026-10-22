# Pi 客服评测入口适配 Implementation Plan

> **For agentic workers:** Use executing-plans inline under the user's main-only, no-question instructions. Steps use checkbox syntax.

**Goal:** 让 AI 客服六案例评测准备、模型配置检查及受控首批执行入口理解当前 Pi DeepSeek，同时隔离历史千问授权。

**Architecture:** 新增显式 target 的 v2 campaign plan；v1 行为保持兼容。v2 授权必须独立绑定新 plan SHA 和完整 target，配置迁移本身不产生付费就绪状态。Langfuse 只执行已有本机健康与认证读取，服务本身不改动。

**Tech Stack:** Go 现有 taskbench/paired-eval/paired-live，Python unittest，现有本机 Langfuse。

**Spec:** docs/customer-service-readiness-2026-10-22.md 的后续实施范围及用户本轮批准。

## Global Constraints

- 只在 main 开发，保留已有工作区改动，不新建分支或工作树。
- target 为 https://api.deepseek.com/v1、deepseek-flash、openai；JEV 仍为 jev-1.13.0。
- 六案例、24 模型尝试、6 JEV 尝试、¥1 / $0.02 上限；输出128、请求16384字节、输入16384 token；沿用的2/8元声明仅用于离线预算演算，不是新渠道费率证据。
- 历史冻结文件不改写；不继承旧模型的付费状态、收费证据或 durable claim，不进行真实模型/JEV调用。
- 新配置检查只读显式指定的私密 env 文件，输出不包含密钥；本机评分/trace API只读。

## Review Focus

- v1授权配v2计划、v2授权配错误地址/协议，必须在副作用前拒绝。
- model target被改为其他主机或含凭据，必须拒绝，不发请求。
- 环境中残留旧模型或旧密钥不能覆盖显式配置；配置不匹配不能视为通过。
- 新计划必须保留独立租户、预算及失败停止，并不能靠准备结果启用付费。
- v1离线工具和历史归档仍可追溯，新结果必须显示其范围仅为六案例。

### Task 1: 绑定新渠道的计划与执行边界

**Files:** internal/taskbench/campaign_prepare.go, authorized_entry.go, authorized_entry_run.go, cmd/paired-live/main.go；相邻测试。
**Interfaces:** CampaignPreparationPlan.Target *PilotTarget；PilotTarget{BaseURL,Model,Protocol,JEVModel}；AuthorizedPilot.ModelSettings() PairModelSettings，无密钥。

- [x] 写失败测试：v2 target正确时生成6案例12臂且allow_paid=false；错误地址/模型/协议/JEV、v1夹带target拒绝；新旧授权不能交叉绑定，未核实账单仍不ready。
- [x] 运行并确认RED，再实现最小v2验证与private pilot模型绑定；首批执行与CLI以绑定模型校验，保持所有旧预算和claim守卫。
- [x] 本地OpenAI替身跑真实handler/隔离fixture/工具/trace/报告，验证首批后停止及HTTP失败不重试；运行相关Go测试。

### Task 2: 按新计划检查显式本机配置

**Files:** scripts/langfuse-eval.py, scripts/tests/test_langfuse_eval.py。
**Interfaces:** check_settings(config, target=None)；check --campaign-plan <v2-plan> 与旧 --preflight 互斥。

- [x] 写失败测试：Pi配置通过，旧千问或错误协议失败；新检查不继承进程环境；无效target在构造网络client前拒绝；旧检查行为保留。
- [x] 确认RED，最小实现；输出plan SHA和settings-only范围，不宣称冻结runtime绑定已验证；25项旧测试与新增测试通过。

### Task 3: 准备结果、审查与交付

**Files:** eval/paired-pilots/2026-10-22-pi-customer-preflight/ 新plan、proposal、manifest；docs及AGENTS更新。

- [ ] 从旧计划复制数值约束到新v2计划，显式注明费率为未核实声明；运行paired-eval生成新proposal。
- [ ] 对显式本机env运行新配置检查，仅本机Langfuse健康/认证读取；归档脱敏结果，确认零推理。
- [ ] Go全项目回归和Python脚本测试，独立review，修复重要问题。
- [ ] 检查差异与暂存范围，提交main，正常推送并验证远端SHA；Hindsight写入并实际读回。
