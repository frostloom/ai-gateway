# V3停止范围的25份claim只读证明实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline, with test-driven-development. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 把已停止V3的9份新claim与原16份具体证明连接成可重新读取的25份历史基线，供后续新独立完整范围使用。

**Architecture:** 新只读descriptor绑定原V2具体证明与V3实际根；固定源码SHA读取顶层原件，再逐项读取账本、record及原生请求/响应。只返回私有具体历史对象，不提供provider、claim、恢复或执行能力。

**Tech Stack:** Go标准库、现有taskbench严格解析及原生usage/账本审计；SDK固定MacOSX26.5。

**Spec:** [原V3停止报告](../../full1220-v3-stopped-2026-10-29.md)、`window_stopped_v2_{evidence,bundle,payload,pins}.go`的具体证明模式和用户当前¥105+$1总额。

## Global Constraints

- 原owner40291已exit1、closed；原scope、9claim及所有原件不恢复/重领/释放/修改。
- 新只读基线必须精确25claim、永久38888170µCNY/308003µUSD、实际1791模型/445JEV；父额105000000/1000000，未分配66111830/691997。
- 原16份证明的74752µCNY/2688µUSD历史hold及原值保留。V3新增1223包982模型/241JEV均完整HTTP200且settled，无新增hold。
- 原前8窗完成，第9窗budget_exhausted；482尝试/481完成/1失败/6838未尝试，597轮/11175未运行。第9窗34记录/90包、71370剩余小于74752下次hold；不将本地blocked模型调用算成实际发送。
- 原3660对/7320臂/11772轮、1851边界、客服50e/v5及两臂knowledge off保持；本单元不创建新scope或付费入口。
- 仅新增下面五文件，不改旧v1/v2/v3执行路径、业务Go或Python评分工具；不创建分支/worktree、不提交旧dirty资产。

## Review Focus

- 不接受只给自报预算/verified=true的JSON，必须重新读取原件并核对具体预算链。
- 在顶层manifest/receipt正确时，缺失或改动任一record/native body仍须拒绝。
- 额外claim10、checkpoint9、窗口10或native sidecar不得被忽略；三个历史根不能交叉或别名。
- 原停止状态、unknown hold及实际发送口径不得被“清理成功”或前端telemetry计数掩盖。
- 首次读取后改旧原件、descriptor或内存base，重新验证必须拒绝；仅开既有.lock的只读fd，不新建锁或修改旧文件。

## Task 1：具体只读加载器与完整审计

**Files:**
- Create: `internal/taskbench/window_stopped_v3.go` — descriptor/types及私有具体加载器。
- Create: `internal/taskbench/window_stopped_v3_pins.go` — 固定顶层原SHA与25claim期望基线。
- Create: `internal/taskbench/window_stopped_v3_bundle.go` — canonical根、namespace、旧16证明前后复验及锁。
- Create: `internal/taskbench/window_stopped_v3_evidence.go` — 九窗claim/summary/receipt/账本/record/native交叉审计。
- Test: `internal/taskbench/window_stopped_v3_test.go`。

**Interfaces:**
- `loadWindowStoppedV3Baseline(path, expectedSHA string) (windowStoppedV3Baseline, error)`。
- `windowStoppedV3Baseline.revalidateWindowStoppedHistory() (fullWindowBaseline, string, string, error)`，第三返回值为V3原根。
- `windowStoppedV3Baseline.windowStoppedPredecessorRootsV4() [3]string`，保留最初两根及V3第三根；避免与旧v3的双根接口混淆。
- `expectedStoppedV3WindowBaseline() fullWindowBaseline`：旧16map复制后追加命名为`full1220-v3-20261028/window-%06d`的9个完整SHA；精确值按Global Constraints。
- descriptor schema `full-window-stopped-baseline.v3`，字段`previous_baseline`绑定原具体V2 descriptor SHA857bc856db8ee7837f9043a97fd98080d7e881c78fae22a6f6a789d0bbbd4ab3、`original_root`、`budget_approval`绑定现有105批准SHA。不得接受caller注入历史验证器。

- [x] 写RED：合法bundle；首16与新增9 claim任一缺失/篡改；顶层一致但子record/body篡改；claim10/checkpoint9/window10/extra sidecar；根交叉/别名；停止状态或模型计数篡改；读后变更；budget改100或币种错配。
- [x] 运行上述新测试，保留断言RED，不把编译/SDK/权限失败当RED。
- [x] 实现固定pins加载、原16具体证明前后重读、既有campaign.lock只读锁及完整inventory；逐窗重算claim永久额、summary prior链与分项，逐请求比对intent/response/usage及actual次数。
- [x] 新record解析复用旧stopped专用合法null处理，不能放宽readiness decoder。原前8窗collection成功与第9窗原false/audit-only状态逐值保留；所有9窗schema仍为原件，不修改数据来匹配新类型。
- [x] 跑定向正常及race：`SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./internal/taskbench -run 'TestWindowStoppedV3' -count=1`，以及同选择器`-race`。检查无实际网络/推理/fixture创建或原件写入。
- [x] 在独立私密test overlay调用真实25份只读加载器，输出实际derived值及旧文件前后SHA清单；不调用actual Open/check或读取provider settings。
- [x] 独立审查后提交/直推该只读单元。全Go20m、native包络、fee及新签名冻结属于后续新执行器门禁，本单元不宣称付费ready。

2026-10-29：只读单元交付；83 normal/159实际overlay/243含overlay race/最终83 unit race通过，root再83通过，独立定向2.286秒通过，无确认P1/P2。57top、25claim、482record/1223native全部读取，9174旧原件前后SHA一致。原factory/paid入口/pricing未改，后续新执行器仍须精确归档全Go20m及其全部门禁，不能据本单元称paidready。早期wrapper误用zsh只读变量导致整命令非0保留，不计成功；空expectedSHA原实现已拒绝的测试直接GREEN，不冒称修复RED。
