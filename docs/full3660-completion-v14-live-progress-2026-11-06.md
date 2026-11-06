# 原3660对 V14 实际执行进度

已获本轮明确600秒政策批准，原答“批准，继续吧”的SHA为2560180d500f5740d102c5e55822b25dc322a9493506c3685cee473c5b6ec4af。模型90秒、JEV10秒、knowledge=off、原题顺序/caps/global¥200＋$4均不变。

唯一新driver已启动一次：tool session11955，driver PID641、wrapper PID/PGID644、Go PID/PGID694。Root为`/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full3660-completion-v14-freeze-20261106-r1`。禁止重启/重发/重领/释放；如观察工具超时，只核验原进程与原始回执，不重新启动。旧V13/21726/69281/69290已永久结束，不续22。

当前仍处于启动历史校验，尚未声称首个模型请求或首窗完成。以本Root新原始回执、账本及本会话progress ledger后续记录为准。原385对为已交付主前缀，旧V13正常174对只作补充；必须完成完整新3275对/456窗，再组成原3660对/7320臂/11772轮并完成Langfuse内容、评分、读回。1851边界独立保留。

签名运行时在等待授权期间丢失后，旧未消费技术材料已逐项physical保留；没有修改旧金融scope/claim。重签不改源码AA/source272f、scope411830/executionfbc，仅换签名公钥及目录引用。当前manifest c87d0459、binary ac969646、binding eee9e3e6、pub40db4bca。实际policy signed RAW为53b64a1d36c32a3ee9201ae624ba9a358607a3ad245e2d5c7bd3456ce6505935；delegated授权RAW为fe700fc0d08e4d5b75ebc2c8dd2fa9d921c1800e966215dea441fd8da281d6dc。全部所需签名均已持久化，未来无需临时RAM私钥；启动后不得再rekey当前scope。

新签名上下文110次前缀回放全部通过，报告a137b1e0、1038.9996秒；新binding独审75a05afa、delegated提案05c983d9、最终prefix/readiness独审73d6d604，限定范围0未关闭P1/P2。Go纯验签及Python require_authority actual context均通过，prelaunch诊断80a4707d。精确归档fullGo4665通过/0失败/8既有conditional skip，复用的source/control/DTO证据均明确对应原字节。

177份旧永久预留¥85.037976＋$0.932373不释放，预留不等于账单；完整新范围连历史、风险、最大extras责任¥198.747697＋$2.805070，保持既定Parent及global预算。初始新model/JEV请求与新claim计数为0，后续实际增量必须由新账本重算，不用计划次数代替实绩。


## 首窗永久技术停止（覆盖上文running状态）

唯一driver11955已exit1，641/644/694均退出并reaped/groupempty，不重启、不续2。第一窗只尝试4臂、完成3臂、8轮，整窗audit-only、主结果新增0、评分全hold；8模型＋2JEV原生尝试，9结算、1个JEV pending held$0.002688，不重试/释放。最后握手timeout1152ms发生于600秒整窗截止附近，collector耗时仅68.673秒；前置校验时间线正在独立核对，不把它简化为供应商物理故障。原cleanup/observer/advanced/payload_verified=false及environment_cleanup=true保持。178永久预留¥85.219758＋$0.936184，累计7245模型1871JEV尝试，预留不是账单。完整3660目标尚未完成；当前只进行必要停止诊断和600秒内实际入口的零付费修复验证。

### 后续零付费组件采样（不重开V14）

- 一次只读 overlay V2 的 Go test 自然 exit0/PASS206.95s；启动 retained177 DATA 102.536985500s，测量完整 fresh177+full-proof 比较102.650s，其中 constructor96.045s/Finish-End6.603s；current full source1.168s，其余顶层每项≤0.127s。CPU profile可由Go pprof解析；嵌套时间不相加，stage1减内部事件不当纯DeepEqual。
- 监督 run receipt **accepted=false / output-validation / ValueError** 保留：Root callback错误期待`completed`，helper正确输出`readonly_components_completed_not_actual_entry_proof`。不改原回执、不重跑、不标整个owned验收通过。Root随后独立核对DATA及116停止原件；独审`profile-v2-data-boundary-review.md` SHA7d1cbfbd4f69191ba2f66202a61ffbf551cab05dc02cdfd279cb3bdfdfde1bec。
- 新采样原件在`.superpowers/sdd/2026-11-06-eval-window-policy/rekey14/stopped-first-window/profile-v2-attempt001/`；`profile-v2-root-output-audit.json`记录精确产物引用。无新paid/financialclaim/actualrole/owner/LF，旧pending与178永久预留不改。
- 此104秒测量**不能解释实际collector计时外约533.993秒残差**，也未覆盖完整actual入口。剩余静态审阅未发现额外177递归或首窗Python并行重验；不把差额猜成源码/历史/睡眠。下一步仅准备完整live-shaped只读前缀探针，collector前强制停止，使用scratch local_stub合成签名/本地observer且全真实source检查，不复用已消费scope角色。
- Hindsight本次权威停止记录写入失败（服务抽取后端401），未重试，不声称已写入；当前以AGENTS、此文档和停止原件为准。
