# 独立现金读取复测入口（2026-10-23）

`paired-live -retest` 实现离线提案 `cash-read-retest-20261023` 的受控入口。当前只完成实现和本地替身验证，没有新增真实模型或 JEV 调用，也没有付费执行授权。原第二批独立复核仍为 failed，原领取和账本保留。

范围固定为原冻结 `balance-020/explicit_scope_followup`，两臂各两轮。全部调用（包括纠正）共享最多 12 次模型 / ¥0.405504、1 次 JEV / $0.002688；达到上限停止。12 次是规划包络，不保证任何模型行为都能完成。独立账本不占用或重置原 campaign，不能沿用旧授权。

## 默认只读检查

从仓库根目录执行：

```sh
paired-live -retest \
  -authorization eval/paired-pilots/2026-10-23-cash-read-retest-offline/authorization.example.json \
  -plan eval/paired-pilots/2026-10-23-cash-read-retest-offline/plan.json
```

示例中的 `human_authorized=false`，版本为 40 个零的占位符。因此只读检查报告 `ready_for_paid_run=false`，不是执行许可。检查只核对授权和计费策略，不加载 env、不访问供应商、不创建夹具、输出或领取，不声称运行配置/遥测或语义复核通过。

`billing.example.json` 的费率和 token 包络核验仍为 false，证据摘要为空。不要直接把这些布尔值改成 true 来绕过证据。后续需要按最终审查版本填写独立授权和可信操作员证据、核对证据文件 SHA；证据是本机操作员声明，不是供应商签名。

## 付费执行前提

先用现有 `scripts/build-paired-live.py` 从已提交且审查通过的最终 HEAD 构建。授权和计费文件均须绑定最终 40 位执行版本、当前 plan 原始字节 SHA、新固定 campaign ID 和 Pi 目标。授权还绑定原失败 journal/review SHA，不改冻结案例。新授权须明确批准独立范围和预算；本次“继续”只推进入口实现。

仅在上述前提均满足后，未来使用 `-retest -execute` 加显式 `-authorization`、`-plan`、`-billing-evidence`、`-billing-source`、`-revision`、新 `-out`、`-release` 和一个或多个 `-env-file`。沿用原入口的 HEAD/构建来源、冻结输入、模型/JEV 地址、全采样本地 Langfuse 与自有夹具检查。`-retest` 与所有 continuation 参数互斥。

计费策略固定 `reserve_full_attempt_stop_on_failure`：JEV 按 64000 token × $0.042/百万 token 全额预留一次 $0.002688，`jev_failure_charges_verified=false`，失败停止、不进入后备推理。账户费率和 token 包络仍须核实，不把预算授权当作计费证据。

在构造夹具或调用服务前，固定身份写入 `.tmp/paired-live-retests/cash-read-retest-20261023.json`，独占创建、文件及父目录同步，0600 文件。更换 plan 格式、授权文件、输出目录或代码版本不能重领；失败/中断也不自动删除、退款或重试。原 `.tmp/paired-live-authorizations/` 与续批领取不变。CLI 负责全局领取，库 runner 的依赖由可信调用者提供，不能把库函数当作跨目录全局防重入口。

成功结构执行停在 `checkpoint_pending`，不自动进入后续案例。回执 schema 为 `authorized-cash-retest.v1`，带独立 ID、旧失败证据摘要、新授权/计划/计费/版本绑定与独立预算；`entry-outcome.json` 另记录清理及回执是否成功。之后仍须实际读回 Langfuse 四轮内容和评分，并独立复核每轮新读取证据；结构通过不代表任务通过。

## 验证范围

新增测试覆盖默认 false 授权、无凭据只读、拒绝混用旧许可/计费/续批参数、版本及证据绑定、固定身份防重复领取，以及本地 OpenAI 替身经真实 handler 和自有 MySQL 夹具完成四轮/失败停止。真实模型修复效果仍未复测。

全项目 `go test ./...` 的18个测试包通过；新增复测测试单独运行通过，包含四轮逐轮成功读取以及模型提供方/JEV失败停止。独立静态代码审查未发现重要问题。所有新增模型/JEV响应来自本地替身。
