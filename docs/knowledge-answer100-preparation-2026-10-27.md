# 100场景真实回答对照：执行准备

当前尚未进行真实回答对比，A/B准确率、每个正确任务费用均为null。既有63.9%是36个直接检索问题的Recall@3，不是回答准确率。本次文件与实现仅属于`codex/customer-knowledge-rag`，不合并main，不改变main1220模型/JEV对比。

## 固定范围

A使用已独立编译核对的main等价提示/工具配置；B仅增加当前12篇知识与检索工具。两组deepseek-flash、OpenAI协议、thinking disabled、输出512、routing=model，JEV为0。对应main参考见`eval/knowledge-reports/2026-10-27-main-baseline/receipt.json`。512为本实验两臂相同设置，不改变main1220的128输出合同。

题集`eval/knowledge/answer100-v1`为100场景：48知识、16未知、16实时业务、20安全；112用户回合/臂，200任务/224定义回合。原72条金标所有字段保持，新增28条；原题参与过召回诊断，不宣称全部为未见泛化样本。多轮包括实时余额再读、套餐后订阅、模型后供应商筛选、账单后7天消费、缺金额补参、预览后取消、空确认。没有合法支付确认后的实际扣款/退款测试，本次不报告该项覆盖。

每任务有独立会话、自己及控制账户，在随机owned MySQL库与私有Redis内执行。每轮比对完整业务/控制快照；预览仍保留原pending证据。安全质量负例不改为通过：关闭本任务HTTP入口，删除独占session并确认不存在，且无业务变更后才能进入下一任务。实际写入、所有权不明、清理失败即全局停止。

技术故障不重试：owned admin专属代理在HTTP非200、错误JSON/截断/传输失败处锁住provider guard，阻止Handler后续付费请求。无可靠类型的工具failure保守全局停止，保留原始结果；不靠中文错误文本猜测可恢复性。这可能使某些无害参数负例不能继续，报告须明确未完成覆盖。

## 费用与永久claim

main共享预算协调入口尚未完成，本实验不消费main既有¥100+$1资金池、不释放其永久预留。候选是**额外且独立最多¥5**，尚未获得人类批准。最新“做100题”不视为突破原总预算的批准。准备就绪后一次询问额外金额，批准前真实模型/JEV/真实claim为0。

官方费率证据：`eval/knowledge/answer100-v1/pricing/deepseek-2026-10-27.json`。全输入按高峰cache-miss ¥2/M、输出¥8/M计保守上界；原始hit/miss另留。上下文整个1M保守上界取1,048,576输入，输出512；每请求最坏hold为2,101,248 microCNY。32768请求字节仅是传输/请求包络限制，不以字节冒充token上界。未知usage/费用保留hold并停止。

最多1120尝试、每回合最多5次、单在途；每请求前意图与最大hold必须fsync。已知usage上界累计+下一最大hold必须≤¥5。**¥5是有条件准入上限，不保证所有100对在任何供应商结果下都能完成**；不能准入时明确列出未运行槽位，不自动补钱、重试或另开目录续跑。永久claim固定为Git common dir下`knowledge-independent-claims/knowledge-answer100-independent-20261027.json`，O_EXCL+文件/目录sync；失败也不释放或重领。

## 执行与证据

新CLI `cmd/knowledge-answer100` 默认只读取公开题集，不加载密钥、连接数据库或创建claim。执行要求独立批准、原话consent SHA、合同SHA、source HEAD、干净RAG分支、当前二进制SHA、suite/corpus/main参考/费率SHA全部一致；endpoint固定DeepSeek官方，传输不自动重试或重定向，不调用Search/Fetch。私密provider/observer文件必须显式指定；密钥和头不进入journal。

```sh
# 只读检查，不需要凭据
go run ./cmd/knowledge-answer100 -root .
# 批准后使用已冻结二进制；具体路径与SHA见私密冻结receipt
knowledge-answer100 -root "$RAG_ROOT" -execute \
  -contract "$CONTRACT" -approval "$APPROVAL" -consent "$CONSENT" \
  -price-evidence "$PRICE_EVIDENCE" -env-file "$PROVIDER_ENV" \
  -observer-env-file "$OBSERVER_ENV" -fixture-admin-dsn "$LOCAL_ADMIN_DSN" \
  -out "$NEW_PRIVATE_OUT"
```

每轮Langfuse必须有trace ID、内容捕获/采样1、evaluation专用release，flush无失败/丢弃。后续真实结果须逐条只读核对trace、检索正文与来源SHA；合成成功不能代替真实读回。

`recording.json`保留实际回答、原始检索审计、业务快照、runtime、provider原始请求/响应/usage、时长及停止信息；journal是独立持久逐请求证据。随机排序`review-package.json`移除arm/runtime/trace/工具审计/快照审计，完整映射单独保存。回答中自然出现引用仍可能提示组别，因此只称隐藏显式标签，不声称完全盲评。

逐任务评事实完整性、无依据断言、安全性；工具状态检查不是语义准确率。A没有知识引用不自动算事实错误。仅完整200任务与实际224回合均满足才Complete；安全提前终止单列task disposition。比较总体与四类正确率、配对差异、错误类型、检索使用/引用覆盖、provider usage/保守配置费用、每正确任务费用与P50/P95延时；实际账单未知不编造。

## 验证与取舍

本轮已验证100场景闭网真实Handler调度：200任务/224回合，222合成provider尝试，200任务业务不变/owned清理通过，59.686秒；这是调度证据，没有真实准确率。首次无导入导致编译失败及脚本语法失败保留在私密过程记录，不充当有效反例；有效边界负例已发现并修复空turn panic、非法assistant响应及运行配置泄漏。

最终独立只读审查发现3项P2：技术故障继续、快照审计泄露、缺回合仍Complete。均在一次修复中补针对性测试并通过；原记录仍保留。完整Go回归/race/最终调度/Langfuse核验的具体退出码与SHA记入最终私密receipt，不以本文件预告代替执行结果。

主要取舍：独立额外预算需用户明确批准；最大上下文hold牺牲可用预算以获得安全上界；无类型工具failure保守停止可能减少完成量；当前RAG不先修召回以免失去原版本对照；本次没有真实确认写操作覆盖。任何真实运行失败均保留，不以更换scope或补跑伪装完整。

最终验证：审查修复后完整闭网矩阵62.924秒，200任务/224实际回合，200无业务变更/owned清理，Complete与task disposition均完整；222次合成provider请求，不是实际推理。Go整套26包通过、0失败，taskbench287.645秒，命令窗口17:23:14至17:28:22。守卫/审阅race2.779秒通过，原macOS LC_DYSYMTAB链接警告保留。旧Python检查首次系统3.9报6个API不支持错误，原测试未改，内置较新运行时37项通过。知识manifest检查通过；knowledge、deploy/agent、internal/agent相对95c1318无diff。Langfuse合成检索内容读回测试8.644秒通过；官方models一次认证GET200、deepseek-flash列出，实际推理0。默认CLI检查输出100/224、approved=false、model_calls=0。

## 用户后续批准20元

用户明确回复“批准20元，开始做吧”。本次独立上限据此改为¥20，保留旧¥5合同验证以免改写准备证据；新真实合同只能选择已批准的¥20，其他范围/1120尝试/每回合5次/单在途/原停止策略不变。原¥5候选未领取，旧pending模板与冻结材料保留；固定canonical claim位置不变，不另开scope。执行前重新冻结源码/二进制/合同与原话授权SHA。
