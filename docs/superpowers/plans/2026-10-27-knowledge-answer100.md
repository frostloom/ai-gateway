# 100场景回答对照 Implementation Plan

> REQUIRED SUB-SKILL: executing-plans。沿用用户既定native实现；不逐步骤再询问。独立新增预算授权为最后一步。

**Goal:** 实际比较main等价策略与当前RAG的最终回答，而非召回分数。
**Spec:** docs/superpowers/specs/2026-10-27-knowledge-answer100-design.md
**Constraints:** 仅RAG分支；原72/知识正文/客服生产规则不改；100场景200任务224用户回合，JEV0；无真实账务写；候选独立¥5尚未批准；未知收费/技术/观测/实际污染失败即停，不重试。

- [x] Task1: internal/knowledgecompare/suite.go + suite_test.go，eval/knowledge/answer100-v1。RED100数量/分类/多轮/原72一致/缺金标拒绝→GREEN，冻结manifest并提交。
- [x] Task2: capability.go/transport.go及测试。RED无独立批准零网络、O_EXCL跨目录不重领、发送前fsync、单在途、原生DeepSeek缓存字段、未知usage/503/EOF/预算不足停止→GREEN。旧guard不放宽。
- [x] Task3: runner.go/CLI及测试。真实Handler+owned fixture，A/B交替，多轮真实工具结果回填、每任务清理证明、SQL不变、缺实际回答不算成功。RED→GREEN，独立审阅包隐藏标签，完整原始usage/时钟，默认check无副作用。
- [x] Task4: 一次fresh-context最终代码审查，Important一次RED→GREEN；相关包、Go完整20m、Docker知识不变验证、本地Langfuse合成读回。冻结执行源码/费率/请求包络/私密false授权，提交推送RAG。
- [ ] Task5: 请求独立于main100+1的候选最多¥5明确批准；获批且全绑定通过后一次执行100对，不重领/续跑。独立语义评分、逐条观测读回、真实差异/失败/成本报告；未批准/未就绪不请求模型。

Task1→Task2传suite SHA/回合schedule；Task2→Task3传opaque capability+guard，不接受bool代替；Task3→Task5保留完整recordings与review映射。无法得到有效费用或包络时保持ready=false，金额null。任务原始证据保存在本计划独立私密目录，所有取舍写ledger。
