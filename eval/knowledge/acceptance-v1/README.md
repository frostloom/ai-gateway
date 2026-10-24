# 知识库评测入口

`go run ./cmd/knowledge-eval -input recorded.json -output report.json` 只汇总已记录结果，不加载密钥、不调用模型。输入类型见internal/knowledgeeval/types.go。A为知识关闭，B为指定版本知识开启，两臂保持model路由及其他运行条件一致。

attempts_complete只在全部尝试和失败均有记录时填true。每个attempt的uncached_input/output/cache_read/cache_creation须来自供应商usage及明确协议归一化；缺失留null，不补0。OpenAI输入含缓存时先分离；Anthropic普通input与缓存字段分别保留。费率为对应币种每百万token的微币值，四项均明确（免费为0）；verified仅在冻结来源核实后为true。计价逐attempt向上取整至微币，不是账单。缓存、CPU/内存、存储与观测成本另报。

独立review缺失时质量不可用；失败成本仍计入。common_pass_micro为两臂均正确题集的全尝试成本，total包含所有题；CNY/USD不相加。录制者身份与原始journal另存，报告不是执行真实性的密码学证明。

CLI拒绝未知字段、额外输入和执行参数；报告的effect_claims_allowed只表示具备比较记录差异的数据条件，不代表正向收益或通过预算批准。local_stub永远false。
