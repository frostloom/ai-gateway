# 新版本缓存分拆结算内核实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline, with test-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 仅在显式新版账本启用报告hit/miss分拆的峰值计价，同时逐字节、逐金额保留所有旧账语义及原准入保护。

**Architecture:** 在现有provider limits增加零值省略的cache-hit费率；旧schema仍禁用该字段并按全部输入计价。新版schema使用有效报告的缓存对计算加权输入费用，准入仍调用原最大未命中成本；本单元不连接actual入口。

**Tech Stack:** Go标准库、现有window accounting/journal/protocol测试；SDK26.5。

**Spec:** [预算设计](../../full1220-after-v3-budget-design-2026-10-29.md)，官方源SHA556097caba8adfeeed6455714bffd731c7f60db520fcee06a922eba5840663a3；私密反事实`.tmp/cache-aware-settlement-design-20261029/counterfactual.json`。

## Global Constraints

- 旧v1账本、v2/v3 scope/manifest/claim、25历史claim及1791/445、38888170/308003的真实基线完全不变；不重算旧known、不释放原hold或永久预留。
- `windowProviderLimits.cost(input, output)`仍是原峰值未命中保守成本；`Begin`最大hold74752µCNY/2688µUSD、次数/usage/失败停止/并发/重放规则不降低。
- 新schema名`full-window-accounting.v2`。旧`full-window-accounting.v1`必须拒绝任何非零cache费率；新版要求模型cache费率为正且不大于原input费率，JEV费率字段必须0。真实新epoch将另行绑定官方40000µCNY/M，本内核不授权任意外部价格。
- 新实际scope/manifest/claim/Open、64m/.69m分配及Go全20m冻结均在后续单元；本单元不创建actual capability、不读provider settings、不发任何模型/JEV/claim/评分/部署。
- 不改protocol归一化或raw usage规则。原Anthropic缺cache字段会归一为hit0/miss=total，费用保守等价全部未命中；不得据此宣称供应商实际报告了命中0。报告可用性由raw metadata另行核对。
- 不改客服50e/v5、知识off、模型/输出128、10秒SDK/2秒观察/300秒窗口，也不动其他dirty资产。

## Review Focus

- 合法hit/miss必须和总输入相等；半缺失、负数、越界等沿用原usage拒绝，不能悄悄折扣。
- 旧schema里即使usage有hit，也必须保持旧全输入峰值费用；旧header canonical JSON无新增零字段。
- 无完整cache对时，不把未知当免费；所有有效分拆费用均不得超过同总输入/output的原最大费用。
- 费用按整数微币种向上取整，hit/miss混合输入先加权再取整，避免两次ceil意外超过原hold。
- 真实25份历史证明在此结构扩展后仍必须通过；不借“修复费率”改写祖先账或新的实际调用次数。

## Task 1：版本隔离的结算成本

**Files:**
- Modify: `internal/taskbench/window_accounting.go` — 新optional int64字段、schema验证及settle调用。
- Create: `internal/taskbench/window_accounting_cache.go` — 纯成本函数。
- Test: `internal/taskbench/window_accounting_cache_test.go`。

**Interfaces:**
- 新字段`windowProviderLimits.CacheHitInputRate int64`，JSON `cache_hit_input_micro_per_million,omitempty`；默认0表示旧计价而不是缓存免费。
- `func (p windowProviderLimits) settlementCost(u *windowUsage) (int64, error)`：先沿用`windowUsageValid`，若费率0或双cache字段缺失则原`cost`；否则`ceil((miss*InputRate+hit*CacheHitInputRate)/1000000)+ceil(output*OutputRate/1000000)`。
- 账本replay与live Settle使用同函数；任何cost验证失败返回error，不追加假settle。旧`cost`和`Begin`路径保持原算法。

- [ ] 写断言RED：新版真实样例input1757/output33/hit1536/miss221，费率40000/2000000/8000000应768µCNY；旧schema同usage仍3778，新最大hold仍74752；新字段0在旧header中省略。
- [ ] 运行新选择器，观察断言失败后再实现（编译/SDK失败不计RED）。
- [ ] 实现新字段/价格验证与纯函数；旧schema拒非零字段、新schema拒缺0/负值/高于miss或JEV非零字段。现有构造器默认schema1不激活新价格。
- [ ] 补mixed/all-hit/all-miss/无cache对/半对/负值/总和不符/最大边界、整数ceil及`settlement<=cost`性质；核对live close与journal replay完全一致，重复/过时ticket和pending失败规则保持。
- [ ] 运行`SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./internal/taskbench -run 'TestWindow(Accounting|StoppedV3)' -count=1`及同选择器`-race`；不运行与本单元无关的DB或真实网络。
- [ ] 私密overlay对真实25claim原证据只读再验证：旧known/预算/次数/SHA逐值不变；另对拷贝的已捕获前缀做新版影子replay，标明排除原halt及未捕获后缀，不称原第9窗恢复/完成。
- [ ] 独立审查无剩余重要问题后提交并直推main。本单元不宣称新完整范围已技术冻结或已执行。
