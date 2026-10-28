# 1220技术停止后的独立历史基线实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在保留首次实际技术停止证据和永久预留的前提下，为原1220三轮完整真实评测准备新的独立版本与预算链。

**Architecture:** 采用显式v2停止前置证据、scope和实际ready合同。先读取原九claim，再核验已停止实际窗口的完整证据；只有从证据推导出的新基线能进入新campaign。逐窗复验同一证据，旧v1/closed合同保持，新scope从头完整运行。

**Tech Stack:** Go、严格JSON、SHA256、现有窗口账本与Ed25519构建签名，不新增服务或付费分类器。

**Spec:** [首次实际停止报告](../../full1220-first-actual-stop-2026-10-28.md)及原1220正式完整任务契约。

## Global Constraints

- 直接main开发/验证/提交/推送；既有自主预授权覆盖常规推进，不逐项询问。
- 原3660对、7320臂、11772定义轮、三轮交错、1851边界另列、两臂知识off保持。
- 知识接入继续按[当前复核](../../full1220-knowledge-review-update-2026-10-28.md)独立设计；主范围的风险尾差不分配给知识付费补充。已有20题是未运行草案，不替代原1220。
- 总额100000000µCNY、1000000µUSD；新历史基线29475706/227183，剩70524294/772817。
- 实际144模型/37JEV；原claim52bbf6e7edf8b66099141db5ac7c6782e45b8afc3bcd9145ea1fd51f936eca48消耗不可释放；原known0/pending74752/false字段不改写。
- 新范围不是恢复旧scope；单owner、显式逐窗、300秒、技术失败即停、无自动重试/重领/磁盘恢复。
- 保留完整Go20分钟原配置及正式fixture；使用MacOSX26.5 SDK。实际build不能用会省略ldflags证明的trimpath，归档需自身detached Git证明元数据。

## Review Focus

- 首次ready验证后，旧claim/原生payload/terminal被删改：首窗和后续claim前均应拒绝。
- 旧negative状态被改success或known0被当成免费：原摘要和永久cap仍是唯一基线来源。
- 原owner未真正结束：必须结合原锁可独占获取及当前进程句柄终态，不只相信可写terminal。
- 新v2误入旧primitive、缺基线字段或旧scope交叉：在新root/claim/upstream之前拒绝。
- 两币种可领取余额与模型未知风险余量：完整整数重算，不把尾差分配给知识补充。

## Task 1：停止证据及新基线的纯验证

Files: 新增`internal/taskbench/window_stopped_baseline.go`、`window_stopped_bundle.go`、`window_stopped_evidence.go`及对应测试；读取现有`window_scope.go`原九claim验证器，不修改旧文件证据。

Interface: `loadWindowStoppedBaseline(path, expectedSHA string) (windowStoppedBaseline, error)`；私有结构保存验证后base、原raw证据SHA及停止bundle绑定，不可由caller JSON构造执行授权。

- [x] RED：缺任一原九claim、固定第十claim、原manifest/terminal/receipt/summary/ledger/payload manifest/原生request-response均拒绝；交叉scope、重复cap、去pending、改false也拒绝。
- [x] 最小实现严格有界普通文件读取与SHA/关联验证。原ledger须closed，原owner须已结束；可获取原锁后只读核验并释放，不能启动旧owner或补写旧证据。
- [x] 推导29475706/227183与144/37，不接收caller累计金额。验证旧完整响应的标准tier和usage，冻结峰价责任3832≤旧hold74752≤旧cap414586；任一无法证明即拒绝新scope，不假设风险尾差覆盖两个未界定请求。保留旧known0/pending，不补settle或释放hold。原stop为失败不影响作为永久预算前置证据。
- [x] 定向223记录及最终新单元53记录通过；联合原账本/协议/传输与原材料最终277记录race通过，独立只读审查无确认P1/P2。
- [x] 提交该只读验证单元；整个v2实际入口仍须Task3精确归档全Go20分钟，当前不宣称新scope冻结就绪。

## Task 2：v2完整scope和campaign链

Files: `window_scope.go`、`window_campaign.go`，新增v2定向测试；旧`window_claim.go`只明确拒绝v2，不扩展未接线primitive。

Interfaces: v2 scope包含不可省略的`baseline_evidence_sha256`；`newWindowCampaignV2`只接受Task1已验证快照。v1序列化和closed路径保持原样。 私有campaign使用密封的`windowStoppedHistory`接口做每次重新验证；实际构造器只传入Task1具体文件快照，测试可在测试文件中实现内存证明，不引入生产test开关。验证同时返回基线、证据SHA及原root；scope、基线及新root与旧root隔离须全部一致。旧实际Open现需v2epoch，旧v1只读及closed记录仍可核验。

- [x] RED：v2缺绑定、交叉入口、旧scope误接收、新基线被覆盖回九claim，以及领取前篡改停止bundle均拒绝且不创建新root。
- [x] 让campaign start、persist和后续复验沿冻结版本读取同一基线；每份v2claim记录十claim SHA及baseline evidence SHA，不仅修改最初ready。
- [x] 验证原3660槽位、三轮轮换、臂顺序及两币种全部cap；模型cap不变，JEV候选增加正常路径余量后，全领累计97897368/932211，尾差2102632/67789，额外未知风险后79208µCNY/67724µUSD保留。
- [x] 验证共享O_EXCL/fsync及v2首份/后续领取、状态变化和旧v1/closed回归；独立审查无确认P1/P2。
- [x] 提交Task2单元；Task3精确归档全Go与实际入口接线仍待。

## Task 3：v2实际ready/session与原Handler替身验收

Files: `window_actual_ready.go`、`window_actual_session.go`、`cmd/full-window-actual`及测试。

- [x] v2 manifest及Open缺停止证据断言RED→GREEN；原材料只读/临时descriptor篡改在启动与逐窗门禁零upstream拒绝。Task1覆盖全部14材料/九claim，旧构建固定SHA与schema不能接受新v2manifest；最终完整有效ready仍须下面精确归档验收。
- [x] 将v2基线验证接入Check/Open/verifyInputs及campaign推进，保持私密settings SHA、固定HTTPS、JEV显式本机route、2秒observer等全部门禁。
- [x] 有界真实Handler替身正常/标准service_tier/未知tier/usage缺失/观测失败与直接清理方法故障通过；原纠正/repair测试在完整Go中实际通过。技术失败无自动重试或下一claim，不恢复原scope；不能笼统说所有故障都只有一次请求，observer/JEV故障前可有正常模型两次。
- [x] 原配置完整Go20分钟、新提交精确归档、source/blob/输入/价格/原生请求包络及签名只读检查；独立复核后冻结全新roots。归档3eb2293，全Go699.159秒/23包pass，签名Check1.931秒通过，细节见[冻结报告](../../full1220-actual-v2-frozen-2026-10-28.md)。

## Task 4：完整实际采集及评分交付

- [ ] 按既有自主授权启动新单owner，逐窗读取并核验原receipt SHA后再显式推进；不自动付费循环。发生技术停止保留新claim和完整失败分母，再独立诊断。
- [ ] 三轮全部3660对采集后完成全部题级独立语义复核，原始规则/附加源规则/质量提前结束/未运行分母均保留。
- [ ] 为窗口结果实现显式版本的Langfuse评分及内容适配，不伪装成旧六案例pair journal；一次发布并只读核验。
- [ ] 交付全量可追溯报告、实际调用/两币种费用与未知费、每正确任务成本、缓存、P50/P95及明确时钟边界，直推main。独立复核全部目标证据后才能complete goal。

## Self-review

原完整范围、十claim前置、每窗TOCTOU、未知成本、旧结果不可改写和独立评分/正文读回均有对应任务。任务1–3不增加真实调用；Task4受原预算和技术停止规则约束。知识整合设计继续单列，不能使用本计划风险尾差。用户已明确授权自主执行，按上述顺序衔接，不新增例行确认。

## Execution ledger

2026-10-28 Task1：原正例及两组filesystem/语义正例先RED；原scope反例曾误用与原值相同的a64，保留失败日志后改成真正不同的9号摘要。账本既有canonical重放抽为有界字节纯函数，原文件路径继续复用同一规则。原14文件及九claim只读验证通过，原known0/pending/false状态和完整预留保留。

Ruling: 将纯责任、跨文件语义和文件/锁边界分为三个聚焦文件；只读单元按定向/race证据提交，不代表整个v2实现或精确归档全Go已完成。最终完整冻结仍在Task3，避免把局部结果当真实完整测评。

Task1最终证据见`docs/evidence/full1220-stopped-baseline-2026-10-28.json`。新增独立要求文件集测试覆盖14停止文件与九历史claim逐项缺失/篡改；故障overlay移除SHA/库存时28项测试失败，正式源最终race无失败/跳过。原材料实际只读loader通过，不属于新推理。

Task2进度：v2数据声明已重算同一3660槽位/原臂序并绑定新基线SHA，新总余额单独校验；旧first-claim/campaign跨v2及旧actual epoch明确拒绝已RED→GREEN。原v1 canonical SHA635bdd4016392c23865c4f9a2e4a7b88fa3cf6291bc0ec473266ce75d63e1855只读重算一致。宽race组5分钟总超时保留，不算通过；新增分组定向race进行中，最终20分钟全Go门槛不变。

Ruling: v2候选在原美元余额内将JEV每窗cap改为2688+(N−1)×100µUSD，N按窗内全部slot（正常同一会话历史路径每任务至多一次；Redis丢失/损坏等例外可能增加调用并提前停止）。模型cap不变；新JEV总705028、全领累计USD932211、余67789，额外未知65后67724。独立整数复核通过，不是新claim/实际费，也不保证全部5886最大JEV尝试；旧scope cap与claim不改。

Task2验证：scope非race332过、定向race9过（35.987秒）、v2 campaign定向race14过（83.283秒）、旧兼容246过/三包pass（taskbench68.532秒）；宽race组302.405秒总超时保留，不计通过。两条原v1 canonical SHA实际只读复算一致。细节见`docs/evidence/full1220-v2-scope-campaign-2026-10-28.json`。
