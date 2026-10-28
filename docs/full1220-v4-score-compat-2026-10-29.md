# 完整1220新范围评分兼容

评分工具接受manifest/scope4，并严格核对25份历史claim的固定映射、原105预算批准、131窗固定caps及模型40000/2m/8m、JEV42000/0费率和原包络。仅新4版计划附accounting2配置来源；不据此证明供应商实际缓存命中、模型使用知识或最终任务正确。缺缓存拆分保守按全部输入未命中计价。

原2/3版计划不增加该字段，旧非零缓存价拒绝。negative、unknown、held、pending、独立复核及单次评分发布规则保持；签名helper仅扩展4/4配对支持，仍不执行实际二进制、不读私钥或settings、不发网络。

77项专项26.106秒通过，含两条断言RED→GREEN；固定SDK26.5的真实Go Ed25519验证2/3/4正例、3/4及4/3混搭、改一位签名负例全部符合预期。原P3九窗计划完整重建canonical JSON/hash/source binding均相同，1442条原意图和持久化读回相同，未重发评分。

原全Python回归首跑因沙箱拒绝既存loopback bind留下1 error，保留原日志；排除该case的离线组只作补充。root以本项目既定本机测试权限重跑**原完整命令**`python3 -m unittest discover -s scripts/tests`，277项42.608秒通过、15项原条件跳过。独立静态审查无确认P1/P2，另5项纯Python1.983秒通过。

这是评分兼容子单元；实际入口集成、最终精确归档全Go和签名冻结仍须通过。本单元新增真实模型、JEV、财务claim、评分POST、部署均0。[固定源码及验证证据](evidence/full1220-v4-score-compat-2026-10-29.json)。
