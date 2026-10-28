# P4停止后的28claim具体只读证明

本单元新增具体Go历史文件证明，独立导出28份永久claim、累计1985模型/501JEV、永久40625891µCNY/323825µUSD及父105000000/1000000内剩余64374109/676175。它不提供Actual/Open、provider或财务领取能力；新epoch5尚未实施，不能称paid-ready或完整业务目标完成。

P4的精确停止形态是两个完成窗、三份已永久预留claim：112条record、250个完整native包（194模型/56JEV），第三窗640648/5274已经由receipt2提前预留，但没有ledger3、report3、checkpoint3或claim4。原summary2.closed=false与原terminal.closed=true同时保留；owner原exit1/relaunchfalse。第三claim未运行也不释放，原旧hold74752/2688、known、failed/needs_review、Successfalse不改。不从这些元数据推断物理停止原因。

`windowStoppedV4Baseline`只保存命名的原25份具体proof，生产factory不接受调用方JSON verifier。新descriptor schema4的SHA为`2f8c83546a2918b85801160f521465c2f53e040073b3590e1ecde53f278ef340`，绑定原25proof `12a9d584…b48a20c`、原105批准`d5062e6f…d2d554`及P4原根。旧25factory在新证明前后重新读取；fresh复验重新执行全部文件校验。四旧根绝对规范且两两不重叠，新类型不promote旧25/16执行接口。

17份固定顶层原SHA与全部112 record/250 native请求、响应、descriptor、store均读回核验。原生usage与每份canonical intent/settle、responseSHA、case/pass/arm/turn/attempt、schema2精确费率绑定；另按40000/2000000/8000000模型加权输入单次ceil再加输出ceil、42000/0 JEV输入成本逐包重算known。祖先schema1仍按原全input峰值，不重价。金额是原配置责任估计，不是供应商账单证明。

P4首窗合法quality_end1虽然observed1、unrun0，不能误删。证明读取其candidate/cleanup原SHA，使用允许合法null的受限数据decoder，重跑纯sidecar验证，核对原record、deleted1/session absent、清理前后数据和EnvelopeSHA；把两个侧文件纳入严格owned库存。原两窗质量negative3/1、quality_end1/0与零unrun保持；独立语义结论不写回原件。原readiness非空decoder未放宽。

现有`.lock`仅O_RDONLY、非阻塞独占读取，不创建或改写。严格campaign/records库存拒绝缺第三claim、新ledger3/claim4/checkpoint3/report3/sidecar及同字节symlink；只检查owned命名空间，不扫描整个冻结根，更不以冻结source中的pyc情况冒称历史source洁净。10309份明确旧证据文件的前后SHA完全一致，未导入冻结Python或改写旧pyc。

SDK26.5/GOPROXYoff定向结果（含子项）：纯unit151项1.316秒；纯unit race151项3.148秒；真实只读overlay普通230项21.129秒；最终overlay race237项153.297秒。均0fail/skip/stderr；两组race保留原LC_DYSYMTAB链接器warning，未隐去。最终race另覆盖显式terminal/summary2.closed重写、third refund/旧schema、receipt2 next缺失、同字节symlink及descriptor读后追加。没有运行全Go/DB/API/owner或创建新财务claim。新的精确归档全Go20分钟留待后续freeze。

TDD初始pins/namespace断言RED、固定模型的weighted768断言RED与合法zero-unrun quality断言RED均保留。初次weighted fixture误用test-model的setup失败和准备Python语法失败另列，不算断言RED。独立审查无确认P1/P2；root负责提交整合，本单元不自行commit/push。

完整五源码SHA、前后清单SHA和测试证据见[机器报告](evidence/full1220-stopped-v4-proof-2026-10-29.json)；私密原始日志、descriptor与10309 SHA清单在`.tmp/stopped-v4-proof-work/`。下一单元才是原3660对/7320臂/11772轮、1851边界另列的独立新scope5，原目标不缩减或拼接旧前缀。
