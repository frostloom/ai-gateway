# 实际窗口传输失败有界诊断计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline and test-driven-development.

**Goal:** 新独立执行器在传输失败时保留有限错误类别及最后已观测HTTP阶段，帮助定位下一次故障；不回填旧失败、不改变执行或计费规则。

**Architecture:** 沿原guard捕获错误处记录标准httptrace的无正文状态快照，写入失败原生响应descriptor的可选字段。错误分类复用现有JEV typed分类器，成功JSON不变。

**Tech Stack:** Go标准库httptrace/sync/time、现有typed错误分类和payload durability。

**Spec:** [实际停止报告](../../full1220-v2-actual-stop-2026-10-28.md)与用户完整1220目标。

## Constraints

- SDK10秒、窗口300秒、HTTP/DB既有超时、失败停止、禁自动重试/重领及全部原hold/claim保持。
- 当前父额度已获批¥105+$1；此单元0真实模型/JEV/claim/部署。
- 只记录有限cause/phase、布尔和耗时，不保存地址、URL、证书信息、头、凭据、错误字符串、正文或回调参数。
- request_written/first_byte不能证明供应商收到/持久化/收费；无trace回调不等于没发请求；phase只表示快照时已观测状态。
- 旧v2原始证据和归档不改；新诊断不宣称修复旧故障物理原因。

## Task 1：失败信息与安全边界

Files: `internal/taskbench/window_transport_diagnostics.go/test.go`、`window_transport.go`、`window_payload.go`、`internal/jev/transport_failure.go`。

- [x] 端到端反例RED：固定deadline错误经guard后payload缺cause/phase，仍保留unknown hold和halt。
- [x] 实现失败才出现的有界快照、并发安全回调、typed类别复用、descriptor验证/深拷贝；正常请求/成功JSON及native bytes保持。
- [x] 覆盖timeout/cancel/TLS/DNS/EOF、阶段、无trace、读取失败/关闭失败、隐私、非法诊断/成功伪诊断拒绝与并发快照。
- [x] 验证原账本/协议/transport/JEV/payload及race；独立审查，不将测试或新GET当历史根因证明。

## Task 2：交付并衔接新完整范围

- [ ] 原有20分钟全Go门槛保留；源码提交后以新独立归档验证，新的16前置claim预算链及实际v3入口须另单元实施。
- [x] 提交/推送诊断单元及真实限制；旧owner49109保持terminal，不做新付费抽样。

## Review Focus

回调重入/并发不污染另一请求，快照不可被晚到回调改写；诊断写失败不得放行第二次请求；成功/旧JSON不增加字段；transport错误类型不通过原始字符串判断；不能以诊断改变未知费用或原halt原因。

## Execution ledger

2026-10-28：端到端及边界断言RED→GREEN；独立P2超限+关闭错误组合丢前缀已RED→GREEN，以原主code控制附诊断，最终245项race通过。旧EOF测试race仅原子化计数，不减断言。一次目录GET200/615ms只证明当前诊断可用。新增旧consumed-v2 epoch封闭反例RED→61项race GREEN，v3实际接线仍未实现。

Ruling: 保留原超限证据优先级，仅记录与主错误code相容的诊断，不因次要close错误让原prefix丢失。

Minor (deferred): valid()校验有限枚举、时间及基本flag约束，未对假设的私有调用方枚举所有phase/flag逻辑组合；生产构造器仅输出自身观测结果，此字段不授权执行或结算。完整新归档全Go仍为后续门槛，当前不称实际ready。

提交前重新运行有限定向race：jev2.094秒、taskbench122.547秒通过；cmd仅编译无匹配测试，平台linker warning保留。完整Go门槛仍待新精确归档。
