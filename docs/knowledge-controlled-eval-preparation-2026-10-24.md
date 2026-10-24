# 知识库独立对照入口准备（2026-10-24）

实施跨2026-10-24至2026-10-25。本轮在codex/customer-knowledge-rag实现独立知识关闭/开启录制与默认只读检查。没有合并main/JEV工作树、部署客服或执行客服真实模型/JEV；没有新增真实付费claim。本轮成果是本地替身工程准备，未测出准确率提升或降本。

## 使用方式与执行边界

仓库根执行：

```sh
go run ./cmd/knowledge-live
```

默认检查冻结scope、72题manifest、语料、逐题画像、144任务交替顺序和实际Handler两臂prompt/tools/合同摘要；不读取env-file、加载密钥、创建claim、连接数据库或请求供应商。检查成功退出0仍输出ready_for_paid_run=false、model_calls=0、jev_calls=0。策略检查不是完整运行环境就绪。

当前固定阻断为cross_entry_budget_uncoordinated、billing_evidence_unverified、execution_authorization_unbound。显式-execute返回2，在读取任何私密env、创建fixture或领取claim之前拒绝。随意改allow_paid/human_authorized、套用旧paired-live授权、换输出目录或revision都不能开启付费执行。本版本也不接受现有历史计费材料冒充新scope。

仅显式loopback替身可录制：

```sh
go run ./cmd/knowledge-live -local-stub http://127.0.0.1:PORT \
  -fixture-admin-dsn '<显式本地MySQL管理DSN，不含数据库>' \
  -out '<新私密输出路径>' -local-cases K001
```

local-stub只允许literal IP loopback的HTTP地址；localhost别名、真实供应商和URL凭据拒绝。-local-cases仅缩小本地调试场景，不产生真实子范围授权。未给该参数时遍历完整72题。新建随机隔离MySQL库、私有miniredis、每题每臂独立账户与会话，本轮结束删除其拥有的数据库并核对不存在；原平台数据库/Redis/容器不被附加或清理。

- 有界供应商替身请求在发送前同步写reserved，响应后记completed/failed；未知usage、HTTP错误或传输错误停止，不自动重试/补跑。原始usage和缓存归一化均保存，价格始终未核实，不能把本地usage当账单。同一local claim在任何初始化之前原子消费一次，失败也不能换Out或新Transport重跑。
- 只支持OpenAI式替身协议，缓存details必须明确，缺失不填0。每任务5次、总720次、每次512输出、65536字节请求限制用于本地验证；请求字节上限不是已核实计费token包络。
- 录制receipt包含答复、原始工具trace、pending、业务及控制快照、真实运行元数据、请求/响应和全部attempt。请求计时为Handler HTTP开始到完整响应体读完；完整runner时钟另记，不称用户E2E。次要遗留：journal的attempt DurationMS目前只到响应头，不含正文等待，不用它声称完整供应商延迟。
- review-package.json使用随机审阅ID且打乱顺序；review-map.json单独保存两臂映射和回复SHA。工具和引用可能暴露知识开关，属于尽力隐藏标签，非完全盲试。两文件均私密0600。
- comparison转换要求完整144任务、独立评分、逐任务Langfuse读回及全部attempt预留/完成配对；partial、缺观测/评分/usage、未知成本不能变成效果证明。当前只接受local_stub，无法通过重标JSON输出recorded_run。知识Recall@3固定首次检索，后续检索单存。

## 费用和后续条件

公开模板保留设计基点revision，allow_paid/human_authorized均false，reserved_cny_micro=null；未伪造最终执行版本、公开费率、token包络或已批金额。AcquireClaim无条件拒绝真实领取，未维护第二套可分配余额。

main仍有按工作树路径领取的历史入口。只给新入口加锁不能证明¥100+$1累计预算并发安全；本轮不改main或JEV来掩盖这一点。后续实际运行前必须将所有可执行领取路径纳入同一互斥/永久累计账本并阻止旧构建绕过，重读最新完整claim清单，然后冻结执行归档、语料/画像、实际payload/token计价和费率证据，再请求本范围一次精确付费批准。不重复询问已批累计总预算，不沿用已消耗授权。

本地替身已经暴露S011问法超出现有有限取消匹配：预置pending可留下，StateOK=false；不把无业务写等同于取消完成。S012空pending虽保持无写，但完整句不是单个确认词，仍可走模型。保留原题、原客服规则与失败；真实模型语义质量尚未测，不能宣称安全集100%通过。

## 验证、审查与证据

修复前完整Go回归退出0，24个测试包通过/27个包无测试。修复后首轮为23通过/1失败，既有TestReserveConcurrentNoOversell在100并发中出现cash lock request lock timed out，非本轮修改路径；首次失败保留，尚未确认根因，不能把旧成功替代修复后全量验收。随后未修改的原测试独立通过（2.74秒，非skip），同代码/5秒锁/标准并行度/默认时限的一次有界完整复验退出0，24包通过、27包无测试；5项opt-in跳过，新runner的Langfuse已另行显式通过。根因仍未确认，不声称永久解决并发超时。

本轮测试和最终独立审查的具体结果与私密日志SHA另见eval/knowledge/control-v1/validation.json。首次RED、调试失败和最终通过日志均保留。Docker用独立随机Compose项目，仅公开知识与合成fixture，退出后清理本轮容器、网络及卷；不更改正在运行的客服。

Langfuse新runner release为knowledge-pilot-synthetic-verification，修复后的最终两trace：0e661bcddbb0288f65879acabf4149e1（关闭，2观测）、c5b20e9917fb51fe88b554dc1919569a（开启，8观测）；10观测逐条读回，exported10/dropped0/failed0，片段正文及来源SHA匹配。首次探针也已读回2+8观测，但相对回执路径错误导致测试失败；原日志保留，不称首次全通过。修复前绝对路径复验的两条trace亦保留。包括首次在内，本轮6合成trace/30观测，无质量评分写入。

Hindsight最初沙箱内连接失败，显式本地只读召回又15秒未响应；本轮未确认入库，长期事实保留本报告。所有金额收益、真实模型质量与JEV组合效果仍未测量。


一次fresh-context独立审查给出三项Important，均动态复现RED后修复GREEN：咨询pending污染、claim进程内换Out/Transport复用、审阅包失败却持久化完成回执。修复后完整pending进入快照，按冻结允许转移表复核；S001/S002合法预览保留，咨询新pending立即停止。回执在审阅包完成后才按最终状态写入，任何错误返回stopped/不完整。未重新审查第二轮，以反例与完整回归验证修复。

## 实施取舍与代价

1. 一律拒绝付费claim，未构建可分配的第二套账本；代价是实际测评要等共享预算集成。
2. 没有加入休眠的付费transport/token费率逻辑；本地仅验证字节/输出/调用及usage归一化，价格保持未知；代价是实际计费包络后续补齐。
3. 经新桥接复用taskbench已有隔离数据库/Redis生命周期；代价是新增桥接API需回归维护。
4. 不改冻结S011/S012或生产取消规则来使测评通过，保留原始状态与失败；代价是真实安全门禁仍可能失败。
5. Recording包含按SHA绑定的完整套件，转换时无需隐式cwd；代价是私密录制文件更大。
6. 真实模型质量、费率/token核实和共享预算未混入本地交付；代价是当前不宣称真实效果或付费就绪。
7. 新增显式loopback CLI便于复验；代价是本地模式不能替代真实供应商测试。

上述为实现者决定，不把它们写成用户逐条确认的事实。唯一递延Minor是attempt响应头计时限制，任务全响应计时仍有效。

私密原始日志与ledger归档目录为本工作树 .tmp/knowledge-validation/2026-10-25-controlled/（0700/0600）。其中verify_evidence.py对照公开validation.json逐文件核对SHA与冻结输入；不把日志存在当作真实执行或质量证明。
