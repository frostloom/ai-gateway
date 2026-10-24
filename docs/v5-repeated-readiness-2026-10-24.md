# v5 重复配对测评：只读聚合与执行准备

本轮完成重复结果聚合器及当前请求/费率证据核对，未进行新的真实推理、领取claim、发布评分或部署客服。新的 `v5-repeated-cohort-20261024` 仍须明确批准一次实际执行；原六例cohort批准已消耗，不能沿用。总预算¥100＋$1已批准，不重复询问总预算。

`python3 scripts/repeated-v5-summary.py --input <显式清单> --out <不存在的新目录>` 只读取显式路径及原始SHA，输出新summary.json。清单schema为 `repeated-v5-summary-input.v1`，包含mode（repeated或historical-single-block）、evidence_origin（actual或synthetic_test）、execution_revision、customer_source_revision、runtime_contract_sha256、plan/receipt引用，以及24或6条按batch顺序排列的journal/report/review引用。引用为 `{path,sha256}`；相对路径基于清单目录。未运行后缀三个引用均为null。合成测试展示完整格式。

固定24对调度、pair/block、原case、实际start顺序、报告行身份、全局trace唯一性、版本和SHA必须吻合。报告行保持model/selective规范顺序，不能拿行位置推断实际执行顺序。独立复核使用原有逐轮正文/requirement绑定检查，真正failed的复核保留为failed；缺失复核、失败批次、原始finish失败、未知指标和未运行对均不进入差值。原始报告及评分不改写。失败记录仍保存已观测成本及耗时；未知和未运行指标为null，永久全范围预留与实际使用分开保留。

四种计时分别取原始server telemetry、HTTP轮次、conversation duration、runner elapsed；浮点累加核对容许1e-6ms误差。runner不含全局DB准备删除、末尾export及人工评分，不称用户E2E。币种分开，JEV按冻结输入$0.042/M、输出免费配置计价，模型使用原守卫计价；不是供应商账单。仅完整独立通过的配对计算selective−model差值，再按case和实际顺序给出median/min/max；固定小队列不能证明统计、因果或总体收益。

11项定向测试通过。独立审查发现并修正两P2：回执失败批次及原journal清理失败原本可误入差值，两个反例均RED→GREEN，复审无P1/P2。最终完整Python182测试通过、15项原条件跳过。首次完整测试因沙箱禁止本机HTTP测试端口失败，原日志保留；取得测试端口权限后通过。Go源码逐字节匹配入口最终18包全量回归来源，未修改客服/执行器Go逻辑，不把先前Go验证称本轮新跑。

历史六例仅通过单block兼容入口读取，6对均通过原独立复核，13→10模型、¥0.051062/¥0.038948另$0.000303072及四种计时与原表完全一致；不能复制为4次重复。11条正常/失败捕获全部回放成功，合成捕获无独立语义复核，因此eligible=0，不能声称真实质量通过。

当前208份正常/纠正完整请求捕获，最大7311字节、1754序列化文本token、128输出、thinking disabled；重新下载官方tokenizer ZIP/JSON摘要与先前相同。此本地文本估计不含供应商内部序列化保证，合成usage不作为账单证据。费率重新核对[DeepSeek官方定价](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/)、[token说明](https://api-docs.deepseek.com/zh-cn/quick_start/token_usage/)、[TypeSafe模型说明](https://docs.typesafe.ai/models)。保守使用高峰未命中¥2/M输入、¥8/M输出；不假设优惠。TypeSafe仍固定jev-1.13.0，不开启long context；失败送达收费未知，维持全额预留及失败停止。settings/Langfuse健康认证和相同代理GET models200仅只读。

待批准范围固定4block、原六例循环移位、24对48臂104轮；每例两种顺序各2。最多248模型尝试/声明¥8.380416＋24JEV/$0.064512；若领取，累计永久预留¥12.300288＋$0.094080，未分配¥87.699712＋$0.905920。整命令5分钟、失败即停、不自动重试/恢复/重领，预留不保证账单或全部完成。客服固定50e3c8d/v5、合同a95cacd，六前置claim完整SHA保持。最终提交后从该HEAD归档构建，并在私密false授权/计费材料中绑定最终执行版本、build provenance和证据SHA；具体来源以 `.tmp/paired-live-private/v5-repeated-readiness-20261024/final-binding-check.json` 为准，生成成功前不能称执行冻结完成。公开模板继续false。

证据目录 `eval/paired-pilots/2026-10-24-v5-repeated-readiness/`；私密日志 `.tmp/repeated-v5-summary-20261024/`。6796份旧私密文件摘要不变。实际累计仍60模型/15JEV，原永久预留¥3.919872＋$0.029568不释放。
