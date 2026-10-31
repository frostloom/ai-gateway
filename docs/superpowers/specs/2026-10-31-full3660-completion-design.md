# 原3660任务的固定前缀与独立后缀组成契约（V10技术设计）

状态：执行代理按既有人类自主推进、批准模型请求、Global200+4及完成3660的授权选定的新技术设计，独立预审33788f53591094b09282b627ae0d4ed7ffd6ed3a02fcb8f2ff2b7af7415f9333。HumanReviewedExactScope=false。本文不是人类逐项审阅或实际执行授权；实现/新技术冻结/新签署尚未完成，不可启动调用。仅新V10明确接受R5正常1..55固定385对为来源；旧V9规则/R5永闭及V7/V8/stub/RAG排除不改写。

## 1. 契约变化与证据范围

原目标仍为原1220题交错三轮，共3660对、7320臂、11772定义轮；1851边界另列，不增加为主实验任务。定义、pass、ordinal、两臂顺序、全部失败/NA分母均保持。

既有`docs/full1220-v9-offline-integration-2026-10-30.md:3`明确写：“已有停止前缀和替身数据都不拼入这份完整样本。”因此不能仅把下述方案说成现有V9天然允许；新组成契约必须显式列出**仅R5正常1..55窗的固定例外**，解释它替代旧单scope完整采集约束的范围，并保留旧V7/V8/stub/RAG排除。`AGENTS.md:25`的“旧V7前缀只补充不并入新样本”及`:37`的“旧566不并入新fullsample”继续有效。

已核原始自主授权及200+4答复中，没有找到“任何未来独立scope均须把所有已验收题全量重跑”的人类明文条款；这不等于已有组成授权。原用户批准的完整任务和预算不缩减。质量契约`docs/customer-service-evaluation-contract-2026-10-09.md:85,91,101,119`要求保留旧标签与分母、服务失败纳入、所有失败重试费用累计、运行前固定变量及三轮分别披露；组成必须满足这些要求，不能看质量选择来源。

固定输入R5：`.tmp/paired-live-private/full1220-actual-v9-freeze-r5-20261031`。manifest SHA `aa9a664d767c080648213e3d98c63c340d2207afea9121d104bbb2775fdc7666`；scope `e38eff5e9b79c7b1ad245cfd7e6eb09400260d7c579affece93ec1281e9976c9`；runtimebinding `541bb3c79b9d11d96edba5470a6f15dde8c6c4067abfdd1a239cb5ad137c58cb`。源commit `4ddd37ad5ba44db630bee61ddfebba5402e09d45`，tree `170b19ea8f6e4f5080ac1a2953cf642fb1c2a7a4`，Sources规范SHA `47486b91579a39e5304c721a0ddc140797b4109864e79406b5b54b6831328d00`，2315文件/515Go。

## 2. 固定组成与不可选择规则

| 来源 | 原ordinal | 原WindowID | 对/臂/定义轮 | 角色 |
|---|---|---|---|---|
| R5正常前缀 | 1..385 | window-001..055 | 385 / 770 / 1268 | 全部纳入，包括失败、NA、原strict负项及2300个已读回score |
| 新独立后缀 | 386..3660 | window-056..511 | 3275 / 6550 / 10504 | 新scope实际执行且独立技术/语义核验 |
| R5停止窗56 | 386..390 | window-056 | 5 / 10 / 22 | 仅技术失败、额外尝试与费用附表；所有评分hold，不提供主样本正面结果 |
| 主样本精确union | 1..3660 | 原511个WindowID | 3660 / 7320 / 11772 | 无重复、无缺失，不把两scope说成单scope完成 |

这里“正常”指完整正常窗口的技术门禁，不指客服质量通过。选择规则在新运行前冻结为：前缀只能使用R5指定55窗；后缀只能使用新指定scope的完整正常窗口；不在多个结果里择优。停止窗不能以“记录已写完”或checkpoint存在升级为完成。后缀若再次停止，按该scope原失败停止政策处理；本文不批准自动另开第三scope替换结果。

每条组成身份键为`(original_ordinal, pass, case_key, arm)`，同时保存来源`scope_sha/local_index/window_id/record_sha`。源题、requirements、context、reply、原状态/终态、review SHA、owner sourceproof、receipt/summary/payload/native、评分intent/ACK/GET证据均引用原字节。前缀2300score不是应发2310的承诺；原正分holes/抑制保持，不重发、不补造。新后缀评分在新once namespace，原prefix registry和ID不改。

## 3. 新scope.v10最小数据形状

保留完整原3660条`Slots`（包括原WindowID）；`Windows`仅精确复制原V9第56..511声明及cap，共456条，不重排、不重新贪心划窗。明确加入composition引用和execution range：`first_ordinal=386,last_ordinal=3660,first_declared_window=56,execution_window_count=456`。先重建原完整V9 schedule/cap向量，再验证精确slice；不能接受任意用户传入range/前缀。完整Slots schedule SHA可保持原值；scope/caps子向量/预算/执行SHA必须新算，不沿用V9全向量SHA。

本地`index=1..456`只用于新claim/checkpoint/ledger/records顺序；其声明WindowID分别为`window-056..511`。建议私有集中接口：

- `executionWindowAt(scope, localIndex) (fullWindowDeclaration, error)`：精确查`Windows[localIndex-1]`，校验新scope的固定映射；不通过字符串拼接推断身份。
- `executionTotals(scope) (fullWindowTotals, error)`：后缀3275/6550/10504和52520/5252逻辑最大调用；完整研究分母3660/7320/11772为单独composition字段。
- `validateV10ClaimDeclaration(d, composition) error`：明确首ordinal386、首声明56、本地链起1；最终3661及精确suffix cap/call和，不放宽旧版本validator。

prefix Slots的WindowID不在可执行Windows中，任何按ordinal或WindowID授权的入口都必须先验证“属于本地index对应声明且在执行range”。不允许只根据Slots存在发permit，也不接受旧R5permit/claim/授权/issue token。新根、本地claim1、全新nonce、typed基线及签名授权从头建立。

## 4. 已确认的接线点与最小改动范围

不全局替换511/3660，不改旧冻结文件或旧epoch规则。新增v10专用scope/actual/typedStoppedR5文件，公共分流只增加精确版本分支；以既有V9路径为模板保留原安全校验。

| 位置 | 当前假设/处理 |
|---|---|
| `window_scope.go:167–185` / `window_scope_v9.go` | 精确重建V9、全量totals；新增V10构造/验证及执行totals，完整Slots并不代表全量执行 |
| `window_claim.go:68–91` | 首ordinal=1、ID=fmt(i+1)、58860/5886；保留旧validator，V10专用声明验证采用386/56及52520/5252，不接可任意改bounds的外部开关 |
| `window_campaign.go:387,496,724,729,806` | 数组按本地index可保留；fullSlots按全局ordinal切片可保留；逐项确认permits/claim合同只来自后缀Windows；末窗用len(Windows)而非511 |
| `window_collector.go:250–257,345` | 原声明ID与local index已有分栏；保留全局ordinal切片，PlannedCampaignTotals改为suffix执行值，另带composition研究分母 |
| `window_actual_session.go:310,381,409–420` | startup硬编码全量totals须版本化；新根目录window-000001保存原声明window-056，不改目录名伪装旧窗56；末窗456，FutureUnrunFromOrdinal仍全局ordinal |
| `window_actual_v9.go` / ready/attestation入口 | V9只准原scope与46基线；新增V10独立准确签名/基线/预算绑定，不复用V9authority或伪称human_exact_scope=true |
| `full1220-v9-window.py` | 固定511及全量summary/claim/token/prefix检查必须新V10消费者：456本地窗、原全局ordinal、suffix分区；旧V9消费者不改 |
| `full1220-v9-owner-template.py` / source helper | startup硬编码全量需新版本；job显式local index+声明ID+执行totals；source replay仍从完整Slots定位原case，逐SHA验证 |
| scores/content factory/最终metrics | 新suffix消费者支持本地chain1..t、6550执行分母和scope-qualified once身份；组成报告单独验证prefix55与suffix456，不把旧R5终端改成done |

还须沿实际调用图集中搜索`fmt window-%`、`index==511`、`range Slots`、固定attempt数/summary partition、最终claim/token、source helper catalog schema和canonical签名字段顺序；上述是已读确认点，不声称已完成全部V10集成审计。组合manifest验证前缀55窗owner proof与旧R5停止终端须同时成立，不能只验正常前缀而隐藏后来停止。

## 5. typedStoppedR5与累计账务

新具体typed基线须验证原46祖先链、R5全部102份永久claim（旧46+新56）、55个正常完成链、停止56及terminal/exit/claim57不存在、全部历史账本/native/原件inventory/锁和namespace。停止窗缺owner genuine sourceproof必须如实保留；typed停止基线只证明历史负债和隔离，不给其质量或正常完成资格。复用现有显式read pass及最终fresh sweep，仍不得跨调用缓存或削弱fresh/身份/旧锁校验。

R5实际summary55和56均显示永久`66472061µCNY/631114µUSD`：这是含已预领取窗56的102份hold，不是花费。H56 cumulative native为1580模型/390JEV，加原3557/918，历史实际总5137/1308。H56额外18模型/5JEV及已结算原用量全部留在费用附表；不能拿费用已结算释放永久hold。已存诊断SHA `2e39e3cb252bd699b7027124a0bb1ed454745bf58000e53ef45f0512400cd9a6`。

从原manifest逐窗独立复算：

| 项 | µCNY | µUSD |
|---|---:|---:|
| 原511窗base | 123827032 | 2037474 |
| 原1..55窗cap合计 | 13635775 | 218602 |
| 新suffix456窗base（相减且逐窗求和一致） | 110191257 | 1818872 |
| typed102历史永久 | 66472061 | 631114 |
| 原risk | 2023424 | 65 |
| cohort最大extra | 1495040 | 53760 |
| 新合计承诺候选 | **180181782** | **2503811** |
| 候选Parent185+3余额 | 4818218 | 496189 |
| Global200+4余额 | 19818218 | 1496189 |

2.503811美元高于旧Parent2.5，故不能称旧Parent已覆盖；Parent185+3是明确新候选，仍在Global200+4内。历史102已含原R5窗56，不能再把其181782/3811加第二遍；后缀新窗56cap再次保留是新实际尝试负债，不能抵扣旧hold。Parent不是再额外加到旧Parent之上，历史基线已包含全部旧负债。

原1..55最大logical6340/634，suffix52520/5252，合为58860/5886；它们是预算最大次数，不是实际调用。前缀和停窗retry/extra均真实0，但新契约仍明确同一completion cohort跨epoch最多20次/model、20次/JEV；typed基线核历史extra/unknown并从剩余额度扣除，不以新scope重置。若发现原数据不是0或累计超过上限，停止准备；不自动提高extra资金或次数。

预算是原all-cold/input+256/output128/JEV+663经验gap/20%copy情景的保守声明，非理论所有maxusage、实际账单或耗时保证。供应商价格/模型版本可变：新freeze必须固定原同模型/参数/协议/端点、适用价格/tokenizer证据；若实际服务模型版本无法证明相同，披露这一可观测限制且提交契约审查，不假造等价。

## 6. 采用前的最小验证与冻结顺序

1. 先独审本契约，明确接受上述**R5固定例外**、全部失败分母和跨epoch限制；没有采用结论不改scope规则。冻结完整prefix输入清单、H55正常proof/评分读回链及H56停止原件，旧R5闭锁不变。
2. 实现V10最小分流与typedStoppedR5，TDD覆盖：prefix permit拒绝；local1→ID56/original386；local456→ID511/ordinal3660且无claim457；slot/armorder/pass/case/requirements变化拒绝；缺/重叠union拒绝；quality选择/stop56入主样本拒绝；历史hold/extra篡改拒绝；取消/超时保持失败停止。旧V9字节兼容及拒旧消费scope不退化。
3. 定向normal/race及新全量精确归档Go回归；真原102历史零付费只读loader/source replay与前后全SHA核对，由root安排独立输出。JSON性能实测只支持待验收候选，不把78.675s外推为业务300s就绪；不得扩大300/90/10。
4. 两source比较：全部customer生产行为文件、prompt/工具/知识off、profile/model/参数、definitions、原slots非窗口身份与窗口ID、cap向量逐pin一致；仅执行器/历史验证变化必须列出精确diff且独审。若customer/路由/知识/题或顺序不一致，拒绝组成，不能只比CustomerRevision标签。
5. 建全新source/binary/manifest/catalog/receiver/执行绑定与技术签名；重新实际prepare、真实新授权验证，旧claim从不复用。新scope源proof/collector/owner/score/content contracts全部同版本闭合后才考虑一次新owner；本文未授此执行。
6. 新每窗正常技术proof后独立非盲逐要求审核，原负项/NA保持，再一次评分及GET核验；失败即永久停止，未完整后缀不能输出full3660 completion。
7. 最后独立completionmanifest验证ordinal1..3660每臂恰有一条指定来源、完整三pass/armorder与定义轮分区，1851边界独立列出。统计分前缀、后缀、合成主样本、技术overhead四栏；延迟/缓存/负载与日期漂移按epoch披露，不宣称同一连续运行。费用含R5停56及所有失败/extra，configured费用不冒发票；原R5报告仍closed未完成。

未决：完整102基线descriptor/源归档/新helper catalog与实际runtime签名字段尚未产生；1851边界的逐项来源union须从原边界清单验证，不从后缀pair数推算。新技术版本的scope dispatch、总数/once身份兼容和真实运行时间仍待实现与独审；没有任何新实验或支付在本设计中执行。


## 7. 执行代理采用裁决与强制机器门禁

选择固定组成以完成原3660全部任务、保留已验收385对；不按质量挑选、不降低数量、不更改模型/分流/知识。实际新scope须用既有自主委托与Global200+4准确绑定并独立冻结，不能沿旧R5许可调用，不声称人类审阅过新精确scope。

canonical primary必须精确7320臂。R5停止56的10臂/22定义轮/23native(18model5JEV)必须在all-attempt reliability与cost分母中机器保留，不仅费用附录。新后缀完整且无其他停止时all-attempt arms=7330、defined turns=11794；primary defined11772与实际observed/quality-end-unrun另算。主样本与all-attempt latency/cache/calls分别披露；每正确任务总成本包含全部cohort失败、NA及技术停止overhead，不能只算成功。旧V7/V8项目工程费用另列、不混主样本。

逐pin核对模型/参数/提示词/工具/知识off/definitions/profile与完整tuple顺序；source commit因executor改变而不同，须列并审精确executor-only diff，不能只看CustomerRevision标签。任何未审客户行为或矩阵变化都拒绝组成。无不可变供应商revision证据时披露观察ID及日期局限，不造等价证明。

新typed102的base.Total延续历史Global200/4，R5Parent180/2.5另验；候选185/3只是新view。后置56真实Go source-data回放0628e35a.../独审6c810456...已完成，仍无normal56 proof/score资格；原v1missing事实保留。固定descriptor2e3d61e0fb383376279b5ea7d7473c48892910844855119a8c2026b063151a47仅data，Go loader/真实一次读取及审查待完成。
