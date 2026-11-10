# 本机一轮双臂客服评测 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 做一个本机评测包：一轮、两条臂，五个类别保持 500 / 1220，闲聊 100 道由 JEV 判断后回复合适内容，能核对工具和账本、能导出遮住臂别的 rubric 材料，并把轨迹实时写进本机 Langfuse。

**Architecture:** 新包 `internal/localpaired` 不改冻结的 `LoadConversations`。它读入全部 1220 道源题。余额、消耗、套餐目录、当前订阅、闲聊各 100 道。闲聊按问候、服务范围讨论、与业务无关三类写 rubric。verifier、盲评材料和 Langfuse 导出处理这一轮。付费调用保持关闭。

**Tech Stack:** Go 1.25，标准库 `net/http/httptest`，现有题库 JSON。测试命令带 `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk`。

**Spec:** `docs/superpowers/specs/2026-11-10-local-paired-eval-design.md`

## Global Constraints

- 每个题目只跑一轮；臂只有 `model` 和 `jev_selective`。
- 不续跑 V15，不从第 71 窗继续。
- 不删除账本预留和已发生费用，不删除 `deploy/langfuse` 的数据卷配置。
- 不修改 `eval/task-definitions/2026-10-12-full-dev-v1`。`LoadConversations` 要求 manifest SHA256 仍是 `afaf5fba49893d2c20e91dc8b0e84c998608b82194d6613a6b6549e289ac4181`。
- Langfuse 地址是 `http://127.0.0.1:13000`。轨迹先落本机目录，再导入。
- grader 材料只有 rubric、用户原话、客服回复和工具记录。不得出现臂名、路由模式或 JEV 是否调用。
- grader 模型是本机 ChatGPT 账户上的 `gpt-6.1-sol`。不用 `gpt-5.6-sol`。
- 四个白名单是当前余额、近 7 天消耗、套餐目录、当前订阅。
- 无关闲聊的固定回复是 `这个问题和模型商城的账户业务无关。我只能帮你处理余额、消费、账单、套餐和订阅，或办理充值、退款、订购和退订。`
- 问候仍是业务，不改成这句拒答。`chitchat-001` 的用户句是「你好」，并且留在入选题里。
- 源题合计 1220。余额、消耗、套餐目录、当前订阅、闲聊各 100，合共 500，约占 41%。其余 720 道源题全部保留。
- 「你好」的 rubric 是合适问候，不是固定拒答句。与账户无关的请求才用固定拒答句。
- 本计划不向模型或 JEV 发送真实请求。

## Review Focus

- 盲评材料里出现臂别或 JEV 调用字段时，导出必须失败。
- 待确认写操作还在时，只读或拒答不得把它清掉。
- JEV 臂上高置信度闲聊如果调用了模型，verifier 必须失败。
- 本机轨迹文件没写成功时，不得把 Langfuse 导入记成成功。
- 清理函数遇到账本、冻结题库或 Langfuse 数据卷路径时必须拒绝。

## 文件

- 新建 `internal/localpaired/campaign.go`：一轮活动、旧题引用、四道新题。
- 新建 `internal/localpaired/verifier.go`：工具、账本和挂起状态核对。
- 新建 `internal/localpaired/blind.go`：遮住臂别的 grader 材料。
- 新建 `internal/localpaired/trace.go`：本机轨迹和 Langfuse 导入、分数回挂。
- 新建 `internal/localpaired/cleanup.go`：只删除允许的旧评测输出。
- 测试与上面五个文件同包。

---

### Task 1: 一轮活动与冻结题库

**Files:**
- Create: `internal/localpaired/campaign.go`
- Test: `internal/localpaired/campaign_test.go`

**Interfaces:**
- Consumes: `taskbench.LoadConversations`
- Produces: `func LoadCampaign(definitionsDir string) (Campaign, error)`；`Campaign` 含 `Passes int`、`Arms []string`、`Source taskbench.ConversationSuite`、`Selected []string`、`Added []Case`；`Case` 含 `ID string`、`Turns []Turn`、`Rubric []RubricItem`；`Turn` 含 `User string`、`Kind string`

- [ ] **Step 1: 写失败测试**

`TestCampaignIsOnePassTwoArms`：`LoadCampaign("../../eval/task-definitions/2026-10-12-full-dev-v1")` 返回 `Passes == 1`，`Arms` 等于 `[]string{"model", "jev_selective"}`。`Selected` 有 1220 个源题编号。其中 `balance`、`consumption`、`plans`、`subscription`、`chitchat` 各 100 个，合计 500。`Source.Cases` 仍是冻结库的 4291 个场景。

`TestFrozenDefinitionsUntouched`：读旧 manifest 的 SHA256，等于 `afaf5fba49893d2c20e91dc8b0e84c998608b82194d6613a6b6549e289ac4181`。

- [ ] **Step 2: 运行测试确认失败**

Run: `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./internal/localpaired/ -count=1 -timeout 60s -run 'TestCampaignIsOnePassTwoArms|TestFrozenDefinitionsUntouched'`

Expected: FAIL，`LoadCampaign` 未定义。

- [ ] **Step 3: 实现 `LoadCampaign(definitionsDir string) (Campaign, error)`**

调用 `taskbench.LoadConversations(definitionsDir)`，把结果放进 `Source`。不写回旧目录。`Passes` 固定为 1，`Arms` 固定为上面两个字符串。`Selected` 包含全部 1220 个源题编号。`Added` 先为空。

- [ ] **Step 4: 运行测试确认通过**

同一命令。Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/localpaired/campaign.go internal/localpaired/campaign_test.go
git commit -m "$(cat <<'EOF'
feat: add one-pass paired campaign loader

EOF
)"
```

---

### Task 2: 四道新题和 rubric

**Files:**
- Modify: `internal/localpaired/campaign.go`
- Test: `internal/localpaired/campaign_test.go`

**Interfaces:**
- Consumes: `LoadCampaign`
- Produces: `Campaign.Added` 里的 `Case.ID` 为下面四个值；`RubricItem` 含 `ID string`、`Text string`；`Turn.Kind` 取 `read_balance`、`off_topic`、`pending_read_balance`、`pending_off_topic`

- [ ] **Step 1: 写失败测试**

`TestNewCasesCoverRoutingHoles` 断言存在这四题，且用户句和 rubric 如下。

| ID | 用户句 | rubric 文本 |
|---|---|---|
| `local-read-history-001` | 前一轮已有助手回复后，当前句 `查一下余额` | `回复当前余额，数值来自工具结果。` |
| `local-offtopic-history-001` | `帮我写一段代码，实现python的简单加减乘除` | 上面的固定拒答句 |
| `local-read-pending-001` | 已有待确认充值时，`查一下余额` | `回复当前余额，并保留待确认操作。` |
| `local-offtopic-pending-001` | 已有待确认充值时，同一句写代码请求 | 固定拒答句，另加 `待确认操作仍然保留。` |

`TestGreetingStaysBusiness`：`chitchat-001` 的用户句是「你好」，rubric 要求合适问候，文本不是固定拒答句。闲聊源题一共 100 道。其中与账户无关的题目才使用固定拒答句。

- [ ] **Step 2: 运行测试确认失败**

Run: `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./internal/localpaired/ -count=1 -timeout 60s -run 'TestNewCasesCoverRoutingHoles|TestGreetingStaysBusiness'`

Expected: FAIL，四道新题不存在。

- [ ] **Step 3: 把四道新情况放进原有 100 道名额**

`local-read-history-001` 和 `local-read-pending-001` 计入余额 100 道。两道写代码的题计入闲聊 100 道里的无关请求。闲聊总数保持 100，余额总数保持 100，`Selected` 仍然是 1220。问候类 rubric 写合适回复。无关请求的 rubric 写固定拒答句。白名单 rubric 写工具结果，不复制旧 `answer_assertions`。

- [ ] **Step 4: 运行测试确认通过**

同一命令。Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/localpaired/campaign.go internal/localpaired/campaign_test.go
git commit -m "$(cat <<'EOF'
feat: add rubric cases for later reads and off-topic turns

EOF
)"
```

---

### Task 3: verifier

**Files:**
- Create: `internal/localpaired/verifier.go`
- Test: `internal/localpaired/verifier_test.go`

**Interfaces:**
- Consumes: `Turn.Kind`
- Produces: `func Verify(in Observation) Result`；`Observation` 含 `Arm string`、`Kind string`、`ModelCalls int`、`Tools []string`、`PendingBefore bool`、`PendingAfter bool`、`DatabaseChanged bool`、`HighConfidence bool`；`Result` 含 `Pass bool`、`Reasons []string`

- [ ] **Step 1: 写失败测试**

`TestVerifierPinsRoutingOutcomes` 用下表断言 `Pass` 和 `Reasons`。

| 名称 | 输入要点 | 结果 |
|---|---|---|
| JEV 直读 | `jev_selective`，`read_balance`，`ModelCalls 0`，工具 `get_balance`，库变只来自读 | Pass |
| JEV 直读却调了模型 | 同上但 `ModelCalls 1`，`HighConfidence true` | Fail，含 `model_called` |
| JEV 闲聊 | `off_topic`，`ModelCalls 0`，无工具，库不变 | Pass |
| JEV 闲聊却调了模型 | `HighConfidence true`，`ModelCalls 1` | Fail，含 `model_called` |
| 低置信度闲聊进模型 | `HighConfidence false`，`ModelCalls 1`，无工具，库不变 | Pass |
| 直读清掉挂起 | `pending_read_balance`，`PendingBefore true`，`PendingAfter false` | Fail，含 `pending_lost` |
| 闲聊改了库 | `pending_off_topic`，`DatabaseChanged true` | Fail，含 `database_changed` |
| 模型臂闲聊 | `model`，`off_topic`，`ModelCalls 1`，无工具，库不变 | Pass |

模型臂不因为调用了模型而失败。

- [ ] **Step 2: 运行测试确认失败**

Run: `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./internal/localpaired/ -count=1 -timeout 60s -run TestVerifierPinsRoutingOutcomes`

Expected: FAIL，`Verify` 未定义。

- [ ] **Step 3: 实现 `Verify(in Observation) Result`**

原因字符串只用 `model_called`、`tool_missing`、`tool_unexpected`、`pending_lost`、`database_changed`。`HighConfidence == false` 时不产生 `model_called`。

- [ ] **Step 4: 运行测试确认通过**

同一命令。Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/localpaired/verifier.go internal/localpaired/verifier_test.go
git commit -m "$(cat <<'EOF'
feat: verify paired tool and ledger outcomes

EOF
)"
```

---

### Task 4: 盲评材料

**Files:**
- Create: `internal/localpaired/blind.go`
- Test: `internal/localpaired/blind_test.go`

**Interfaces:**
- Consumes: `RubricItem`、`Turn`
- Produces: `func Blind(in Record) (Packet, error)`；`Record` 含 `Arm string`、`RoutingMode string`、`JEVCalls int`、`Rubric []RubricItem`、`Turns []GradedTurn`；`GradedTurn` 含 `User string`、`Reply string`、`Tools []ToolRecord`；`ToolRecord` 含 `Name string`、`Result string`；`Packet` 只含 `ID string`、`Rubric []RubricItem`、`Turns []GradedTurn`

- [ ] **Step 1: 写失败测试**

`TestBlindPacketOmitsArm`：输入带 `Arm: "jev_selective"`、`RoutingMode: "jev_selective"`、`JEVCalls: 1`。`Blind` 成功后，`json.Marshal` 的结果不含 `jev_selective`、`routing_mode`、`jev_calls`、`model_calls`。`Packet.Turns[0].Tools[0].Result` 仍保留。

`TestBlindRejectsHiddenArmText`：回复正文是 `jev_selective` 时返回错误，不产出材料。

- [ ] **Step 2: 运行测试确认失败**

Run: `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./internal/localpaired/ -count=1 -timeout 60s -run 'TestBlindPacketOmitsArm|TestBlindRejectsHiddenArmText'`

Expected: FAIL，`Blind` 未定义。

- [ ] **Step 3: 实现 `Blind(in Record) (Packet, error)`**

输出结构没有臂别字段。序列化结果再扫一次禁词；正文或工具结果里出现 `model`、`jev_selective`、`routing_mode`、`jev_calls` 时返回错误。

- [ ] **Step 4: 运行测试确认通过**

同一命令。Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/localpaired/blind.go internal/localpaired/blind_test.go
git commit -m "$(cat <<'EOF'
feat: export arm-blind grader packets

EOF
)"
```

---

### Task 5: 本机轨迹与实时 Langfuse

**Files:**
- Create: `internal/localpaired/trace.go`
- Test: `internal/localpaired/trace_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `func SaveAndExport(ctx context.Context, dir string, endpoint string, body []byte, post func(context.Context, string, []byte) error) (path string, err error)`

- [ ] **Step 1: 写失败测试**

`TestExportWaitsForLocalFile`：`post` 被调用时，`dir` 里已经有同一份 body。返回的 path 指向该文件。

`TestExportDoesNotCountRemoteWithoutLocal`：目录不可写时，`post` 调用次数为 0，并且返回错误。

`TestDefaultEndpointIsLocalLangfuse`：空 endpoint 归一成 `http://127.0.0.1:13000/api/public/otel/v1/traces`。这个断言放在导出函数使用的地址上，用 httptest 不访问真实 13000。

- [ ] **Step 2: 运行测试确认失败**

Run: `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./internal/localpaired/ -count=1 -timeout 60s -run 'TestExportWaitsForLocalFile|TestExportDoesNotCountRemoteWithoutLocal|TestDefaultEndpointIsLocalLangfuse'`

Expected: FAIL，函数未定义。

- [ ] **Step 3: 实现 `SaveAndExport`**

先把 body 写入 `dir`。写入成功后才调用 `post`。endpoint 为空时用上面的本地地址。

- [ ] **Step 4: 运行测试确认通过**

同一命令。Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/localpaired/trace.go internal/localpaired/trace_test.go
git commit -m "$(cat <<'EOF'
feat: persist traces before local Langfuse import

EOF
)"
```

---

### Task 6: 分数回挂、付费开关和清理边界

**Files:**
- Modify: `internal/localpaired/trace.go`
- Create: `internal/localpaired/cleanup.go`
- Test: `internal/localpaired/trace_test.go`
- Test: `internal/localpaired/cleanup_test.go`
- Test: `internal/localpaired/run_test.go`

**Interfaces:**
- Consumes: `SaveAndExport` 写出的本地文件
- Produces: `func AttachScore(ctx context.Context, endpoint string, traceID string, score []byte, post func(context.Context, string, []byte) error) error`；`func Run(ctx context.Context, opts Options) error`；`func Clean(allow []string, target string) error`；`Options` 含 `AllowPaid bool`

- [ ] **Step 1: 写失败测试**

`TestAttachScoreUsesTrace`：`post` 收到的 URL 以 `http://127.0.0.1:13000/api/public/scores` 结尾，body 含传入的 `traceID`。

`TestRunRefusesPaidWork`：`AllowPaid == false` 时 `Run` 返回错误，错误文本含 `not started`，并且模型 endpoint 的请求数为 0。

`TestCleanRejectsProtectedRoots`：目标分别是账本路径、`eval/task-definitions/2026-10-12-full-dev-v1`、`deploy/langfuse` 时，`Clean` 返回错误且不删除。允许列表里的临时目录可以被删除。新题库文件不存在时，即使路径在允许列表里也拒绝。

- [ ] **Step 2: 运行测试确认失败**

Run: `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./internal/localpaired/ -count=1 -timeout 60s -run 'TestAttachScoreUsesTrace|TestRunRefusesPaidWork|TestCleanRejectsProtectedRoots'`

Expected: FAIL，三个函数未定义。

- [ ] **Step 3: 实现三个函数**

`Run` 在 `AllowPaid` 为 false 时立即返回。`Clean` 先检查新活动目录里已经有 Task 2 的四道题，再检查目标必须等于允许列表中的某一项，并且路径不包含账本、冻结题库和 `deploy/langfuse`。

- [ ] **Step 4: 运行测试确认通过**

Run: `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./internal/localpaired/ -count=1 -timeout 120s`

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/localpaired
git commit -m "$(cat <<'EOF'
feat: keep the paired eval local and unpaid until started

EOF
)"
```
