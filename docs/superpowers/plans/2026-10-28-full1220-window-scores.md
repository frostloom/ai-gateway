# 完整1220窗口评分与正文验证适配计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline, with test-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为正在采集的完整1220窗口生成可审计的独立评分，发布一次并只读核验；原记录、失败和未运行分母全部保留。

**Architecture:** 新版本适配器消费签名冻结manifest、显式SHA锚定的summary/receipt、逐臂record及独立review；不伪装为旧六案例journal。复用旧Langfuse客户端的受限通信，采用独立计划、原capture规则正文投影、逐ID持久化发布意图和独立只读verify。

**Tech Stack:** Python标准库、现有`scripts/langfuse-eval.py`客户端、冻结Go源码的内存capture投影；不增加推理模型或外部依赖。

**Spec:** 用户原1220完整真实对比及Langfuse评分/内容读回目标；[v2冻结](../../full1220-actual-v2-frozen-2026-10-28.md)和进行中的`full1220-window-independent-review.v1`原始独立审查。

## Global Constraints

- 原归档3eb2293的owner49109已终止exit1；原scope不续/重领，当前对已停止数据操作，不改变原归档、owned records/campaign或供应商请求。
- 原3660对、7320臂、11772定义轮、三轮交错、1851边界另列、知识两臂off不变；用户父预算已调整为¥105+$1，16历史claim永久预留33395934µCNY/259511µUSD；本适配器不能发模型/JEV请求或创建实际预算claim。
- 不把needs_review/structural_pass视为语义通过；不重写原Successfalse、failed或negative要求。
- 原确定性负例若独立语义已证实正确，不能写成语义错误：保留真实semantic结论及分歧元数据，抑制正分或明确unknown/omit；strict仍保持负例。
- 区分原确定性结果、独立语义任务、严格组合结果与评测端分歧；五份千分位部分匹配疑例需独立证据确认，不静默升分。原negative不发布通过分。
- 未运行无trace不得伪造trace评分；技术失败、质量提前结束及未运行后缀继续进入原计划分母。
- 内容验证按冻结源码的脱敏/裁剪结果比较，不冒称未脱敏原文、LLM请求正文已进入Langfuse或模型实际使用资料。
- 不在测量窗口内进行批量评分写入；读取与发布在owner空闲边界或采集结束后进行。
- 用户已授权main直接开发/提交/推送及自主推进，无例行确认。

## Review Focus

- 相同case在三轮/不同arm重复，必须保留ordinal/pass/window/trace，不能覆盖或串分。
- review声明verified=true不足，必须重算实际record/回复/要求绑定及完整身份集合，漏一项拒绝全部发布。
- 原failed与独立语义金额正确出现分歧，不能伪改原结果，也不能将解析误判当成回答真实错误。
- 正文投影被篡改，不能只信投影SHA；须从原记录通过冻结capture规则重新生成并比较。
- POST已发送但ACK/verify失败，保留意图并只允许读回，不能重发或借新receipt重发同ID。
- 防重发锁使用稳定逻辑身份scope+window+pass+ordinal+arm+trace+metric；review SHA、计划修订、输出目录或外部score ID变化都不得绕过该锁。

## Task 1：纯计划及严格证据绑定

Files: 新增`scripts/full1220-window-scores.py`和`scripts/tests/test_full1220_window_scores.py`。

Interface: `build_window_score_plan(manifest_raw, summary_raw, receipt_raw, records, review_raw, expected_hashes) -> dict`，返回独立schema的计划、全部计划分母、trace绑定、原状态和独立审查来源。

- [x] RED：完整双臂正例；重复case跨pass不覆盖；错SHA/错scope/错trace/错ordinal/缺review/缺requirement/改reply/同义改写requirements均拒绝。
- [x] 实现完整字段绑定和新评分namespace，确定性状态与独立评分分开，不接受旧pi-first-live review或journal替代。
- [x] 覆盖原failed、not_started、partial/技术失败、quality_end未运行后缀、semantic not_assessable和确定性/语义分歧；禁止给原negative发通过分。
- [x] 运行纯计划测试及原Langfuse脚本回归，提交可独立审查单元。

## Task 2：冻结capture规则投影与实际trace校验

Files: 新增`scripts/helpers/full1220_content_projection_test.go.txt`；扩展上述适配器和测试。

Interface: `project_window_content(...)`在验证后的精确归档用固定helper和内存exporter调用原`Observer.content()`，返回expected JSON及原input/reply SHA；`validate_window_traces(plan, projection, client)`读取原trace并校验全部身份、turn索引和正文。

- [x] RED：密钥/邮箱等脱敏、UTF8裁剪、4096-byte包装、空回复及被改投影；source/record不同或helper不可信拒绝。
- [x] helper不创建网络exporter/trace，不读取凭据，仅在独立结果目录O_EXCL写出投影；Go投影使用MacOSX26.5 SDK。
- [x] 验证root/pair/pass/order/release/evaluation、所有turn和正文；缺失/延迟读回保留未验证，不重发trace或推理。
- [x] 当前私密首窗prototype已验证56trace/231obs/60turn；正式适配器仍须上述测试和独立审查，不能直接把prototype当产品实现。

## Task 3：单次发布与只读核验

Files: 扩展适配器/测试，增加相应使用说明与公开脱敏结果证据。

Interfaces: CLI显式`plan / publish --execute / verify`；只读默认。publish先验证全部trace和plan，再在该scope固定发布目录以O_EXCL领取每个score ID、fsync intent、POST一次、fsync accepted；verify不执行POST。

稳定逻辑身份锁先于score ID领取；同一逻辑身份已有intent而ACK未知时，更换review/计划/ID也拒绝发布，只能verify。该锁不等于付费推理claim，不允许借它启动推理或采集。

- [ ] RED：任一绑定错误零POST；重复发布、HTTP错误、ACK截断、写意图/落盘失败、verify延迟均不自动重发。
- [ ] 复用旧受限本机evaluation客户端，严格校验settings权限和目标，不允许端点/trace/score ID替换。
- [ ] 原始Go状态与独立语义评分保留各自命名和provenance，review SHA/执行版本/record/receipt/范围进入元数据；原negative仅负例/未知分。
- [ ] 纯替身测试、完整相关Python回归和独立审查通过后，以已结束窗口实际发布一次；所有ID读回后记录，不改采集状态。
- [ ] 为最终3660对报告汇总完整执行/未运行分母、两币种已知/未知成本、每正确任务成本、缓存、各时钟P50/P95；部分窗口不称总体结论。

## Execution ledger

2026-10-28：窗口实际采集与独立复核已开始。首窗私密投影直接复用冻结Go content()在内存exporter生成，真实Langfuse56trace/60turn正文及231obs元数据只读通过；0评分。新窗口格式尚无正式评分适配器，旧脚本仅可复用通信客户端。源码抓到cashClaim不含千分位的风险，正在用五份原记录作只读复现；保持当前冻结执行器与原所有negative。

2026-10-29 Task1：纯计划与30项测试交付，230项Python回归/15原skip/0fail-error；原六窗334record/1268要求全部重算匹配，269/63/2语义与259/75严格口径不改。独立P2首窗observed/quality互换已root复现并RED→GREEN：首窗分项精确，后窗当前quality下界，prior分项显式pending链验证；不假设prior quality=0。无第二轮审查，修复后完整suite与root30测试通过。Task2/3仍pending、publish_ready=false、0POST。

2026-10-29 Task2：固定helper在精确归档副本调用原content()，原source/staged/records/helper/可信Go SHA前后校验，opaque proof拒绝普通JSON；纯测试53项通过，完整Python253项/15原skip/0fail。真实预检root.git误拒和首GET布尔metadata表示误拒分别保留并最小RED→GREEN，独立定向复核无确认P1/P2。最终新v3首窗56trace235obs60轮逐源正文/元数据/父链只读通过，评分仍0/publish_ready=false；其余窗尚待实际内容读回，Task3未完成。

2026-10-29 Task3实现已固定待实际发布：逐窗原summary SHA链重算至停止窗9通过；固定stdlib Ed25519 helper对原manifest/attestation/binary真实正例通过、改1位signature反例拒绝，不执行消耗的actual binary。72专项、完整Python272项/15原skip/0fail/41.409秒。独立P2发现verify只glob现存intent子集误报完整，RED复现12缺1仍true；改为先持久化原完整batch清单，exactset核对，原复现变unverified且新review仍验证旧完整payload，3项独立复核通过。score wire剔除内部logical_identity，固定scope registry/once-only intent与受限client均实现；尚未实际POST，最终全量报告未完成。
