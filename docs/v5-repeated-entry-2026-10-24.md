# 当前 v5 独立重复队列受控入口

本轮实现 `paired-live -repeated`，固定身份 `v5-repeated-cohort-20261024`，使用独立 `v5-repeated-cohort-authorization.v1`、`v5-repeated-cohort-billing-attestation.v1` 和 `authorized-v5-repeated-cohort.v1`。公开授权、费率/token证据仍为 false；不加载凭据的默认检查可用，尚未冻结最终实际执行版本/计费证据。本轮没有新增真实模型/JEV推理、claim、评分或部署，累计保持60模型/15JEV。

## 固定调度与版本

沿用客服50e3c8dab14b4b6cc25322fd340a274e6970200b、合同v5 a95cacd041830012619a0f4d7fd8208c184aa9e8d45b7ee05afaa239a30e1e8f、原六例和完整问法/画像/128输出。绑定显式顺序离线验收的 manifest/validation SHA、5c2f669诊断及100+1预算摘要。新增schema不能通过历史cohort/retest/首批/续跑入口；历史scope也拒绝新增的重复调度元数据。

4个block，各六例一对；每block案例按原序循环左移block个位置，block0/2每对model先，block1/3每对selective先。合计24对、48臂、104轮，每例两种臂顺序各2次。原case_key不改，独立batch-001..024是租约/输出身份，block-00..03和block-NN-pair-NN是重复身份；pair/block/order写入配置摘要、报告、回执及任务和子span元数据。每臂从相同冻结画像创建独立tenant/session，不复用业务状态或trace。

通用准备器只对这个固定scope允许跨batch重复case_key，并完整校验24条固定schedule；旧scope仍禁止重复case或重复元数据。不能靠重算plan/auth摘要、调整案例/顺序/pair/block/上限扩张范围；正反顺序仍走同名routing与原工具，不换标签或定义。

## 永久claim与预算

执行前必须同时匹配原确认、诊断重测、取消、补充后确认、退款和上一轮六例cohort六份消耗claim的原始完整SHA。最后一份为b49808feb6a124c60c7f3fd88a7965589368a0ce8d8f4336e94ebaee9a025d34。缺失或变化拒绝且不创建新claim。新固定路径采用O_EXCL、0600及fsync；失败、换输出/版本不重领，不释放预留。

每对模型上限按原案例[6,12,10,10,12,12]，每对JEV上限1，四block为248模型/声明¥8.380416+24JEV/$0.064512。若将来明确批准并领取，整范围立即永久预留，累计¥12.300288+$0.094080，100+1未分配¥87.699712+$0.905920；准备阶段仍是原预留¥3.919872+$0.029568。本轮未分配额外范围、不是通用预算管理器，也不把声明费率或预留当供应商账单。

整条命令沿用5分钟时限，不逐对续时；包络不保证所有合法模型循环完成。新scope独立开启每臂结束、finish journal持久化后检查确定性fact mismatch及observer flush/Failed/Dropped，失败即阻止下一臂；另保留提供方/JEV/usage/结构/执行清理失败停止、无模型回退或自动重试。原cohort/default不启用新增的两项策略。语义失败可能在独立复核才发现，不能倒称此前已执行的请求未发生。

回执区分整个248/24预留和已开始prefix租约，保留每对not_started/failed/completed_pending_review；缺usage保留未知成本。仅全部24对运行结束才checkpoint_pending，仍需逐臂独立语义复核，不能把needs_review当成功。回执写入失败返回stopped/receipt_write，不覆盖已有文件。

## 离线验证

新scope/claim门禁先观察未实现的失败，随后发现生成模板误用id而非batch_id，按原schema纠正模板，旧材料不改。真实Handler+隔离fixture+官方目标自定义模型transport/FakeJEV的正常与纠正路径分别完成88/24和120/24本地尝试；104轮状态断言、48个独立账户/trace以及每对实际start顺序、配置/报告/遥测元数据均核对。合成usage只测试守卫，不作计费或真实质量证据。

正向第二对提供方失败3模型/1JEV、JEV本地EOF6/2、缺本轮读取5/1、缺usage3/1即停止；反向第七对提供方失败23/7、JEV失败22/7即停止，后续对无目录/请求。取消首个请求后1/0停止。第二对首臂遥测503失败6/1停止，保留该对Finished1/Unrun1；回执文件已存在时88/24后返回stopped。工厂构造数与供应商请求数不混称。

独立审查发现两项P2并通过反例修正：只看StructuralPass会遗漏事实mismatch；整对结束才flush会使首臂观测失败后仍启动第二臂。事实错误确认金额999的反例先出现两臂均运行，修正后仅一臂、事实错误和未运行臂都保留。observer反例从首对fastpath扩展到两臂均需模型的第二对，证实第二臂无start/请求。独立复审无剩余P1/P2。

最终完整回归、旧证据完整性、隔离库及源码SHA见 `eval/paired-pilots/2026-10-24-v5-repeated-entry/validation.json`。原始替身请求、journal/report、账本与返回回执在 `.tmp/repeated-v5-entry-20261024/capture/`，不把本地替身计数当真实调用。

下一单元是只读重复结果聚合器、完整请求/token与当前费率证据、提交后归档执行器和最终false授权/计费绑定，之后才可请求新精确付费批准。本次不请求付费、不沿用此前cohort批准，也不宣称统计或真实流量总体收益。
