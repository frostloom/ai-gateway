# Pi 第二批检查点续跑入口（2026-10-23）

新增 `paired-live -continue-from <首批原目录> -review <独立复核 JSON>`，只选择 `batch-002`，完成后再次停在 `checkpoint_pending`。本轮仅开发和本地替身验证，没有真实模型或 JEV 推理，没有产生第二批付费授权文件、执行领取记录或推进真实检查点。

## 校验与账本

只读检查要求显式传入原 Pi authorization、plan、billing-evidence 和 billing-source，并验证首批原始 receipt、成功 cleanup outcome、重新生成的 run 报告、逐条通过且哈希匹配的独立语义复核。缺失、重复、改变的复核条目均拒绝。原始模型计费账本必须全部结算且 token、调用数、预留和配置计价与回执一致；JEV 账本必须有一次完整成功结算。未决、失败、损坏账本、锁文件、预留标记不一致或额外 campaign 项均拒绝。

真实首批归档只读兼容检查通过：原模型 2 次、JEV 1 次，`ready_for_paid_run=false`、`continuation_permission_required=true`；不读取 env、不连接数据库、不构造付费客户端、不创建文件。检查的计费材料是已有操作员声明，不是新增供应商账单确认。

执行路径复用首批原 campaign 目录与原绑定，包括原执行 revision；新子批次绑定新审核 revision。首批永久预留不退款，因此第二批预留后累计上限占用为模型 10 次、¥0.337920，JEV 2 次、$0.005376。原实际用量与新批次用量分别写入新 receipt，不把预留当作实际花费。旧 receipt、run 和授权 claim 保留；原 campaign 账本追加第二批预留及完成记录。第二批目录仍在原 campaign 下，新 `-out` 仅保存新 receipt/outcome。

## 显式执行所需授权

另需 `-continuation-authorization <本机授权 JSON>`，格式如下；以下是说明模板，不能直接作为已批准授权：

```json
{
  "schema": "paired-continuation-authorization.v1",
  "human_authorized": false,
  "batch_id": "batch-002",
  "plan_sha256": "PLAN_FILE_SHA256",
  "previous_receipt_sha256": "CHECK_OUTPUT_RECEIPT_SHA256",
  "review_sha256": "CHECK_OUTPUT_REVIEW_SHA256",
  "automatic_paid_retries": false
}
```

只有用户明确批准第二批执行后，受信操作员才可生成相应 true 声明。它不提高六案例总预算或第二批 7 模型 / 1 JEV 的上限，不替代已有计费证据。执行仍要求 `-execute`、新输出目录、release、匹配 HEAD 的审核归档/VCS 构建与显式 env 配置。

入口在 fixture 创建前核对原 canonical claim 的 output 与首批目录，并用独占、同步写入的 `.json.batch-002` 领取记录锁定同一计划的第二批。不同输出路径不能重置执行；失败也不删除或自动重试。账本重开再次校验，原 campaign 已出现第二批时拒绝。该约束针对受信本机操作员，不提供跨机器中心化执行控制或防恶意文件篡改证明。

## 验证范围

新增测试用真实 handler、隔离 MySQL fixture、私有 Redis 替身和假模型/FakeJEV 验证首批→复核→第二批、累计预留、模型失败停止、重复执行拒绝、损坏计费账本及预留标记拒绝；CLI 验证授权各字段绑定、原领取记录保留和换路径防重放。独立代码审查发现的计费账本和预留标记缺口均以先失败后通过的测试修复。

测试期间启动了项目已有 MySQL、Redis 容器，保留原数据卷和配置；未启动真实推理。Hindsight 在受限环境无法连接，长期结论先由本文件保留。

最终 `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./...` 退出 0，18 个测试包通过；独立复审确认两个发现均修复，无剩余重要问题。工作区原有前端资产、integrations、临时文件均保留在本次提交范围之外。
