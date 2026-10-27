# 小知识库成本优化执行计划

Spec: docs/superpowers/specs/2026-10-27-knowledge-cost-design.md。用户已要求在此分支自主思考并验证；不影响main，不追加既有20元上限。

1. Corpus受限resident原文与Hit稳定编译，失败测试超限/副本/完整事实后实现；Toolbox显式resident与默认tool兼容，prompt/tool/runtime身份区分；Docker env opt-in。
2. 请求中的常驻知识逐段观察与来源哈希，关闭捕获不漏正文；测试伪用户KB/工具授权/真实事实仍读业务工具/不造tool消息。
3. cost-study新范围继承旧完整200/224账目308call/1312208micro、不重领/不释放；全新treatment namespace，A=tool B=resident；独立新批准绑定与canonical claim，默认不执行。拒绝假complete/预算重置/跨scope重发测试。
4. 相关包、真实Handler闭网场景、一次最终独立审查，冻结/提交/推送后执行完整100新配对（用户本次已授权），预算20总额不追加。
5. 同一rubric隐藏组别独立评分；核验全量Langfuse实际知识正文与请求；报告新tool/resident与历史main参照的正确率/原生usage/成本/冷缓存情景/延时，保留失败与旧结果。只有达到目标才推荐默认切换，不自动部署或合并main。

已完成实现/验证：resident完整原文3171编译字节、12段，默认tool回切runtime JSON一致；真实请求/内容关闭/伪用户知识/来源SHA测试通过。五个直接受影响包全部通过，知识专项回归及真实Handler闭网新两臂测试通过。独立最终审查1个P2“完整基线可欠记账”已用1call/360micro伪完整反例RED，入口锁定308/1312208且正fixture改完整账目后GREEN，漏调用/假complete拒绝。新study保留父20总额，旧结果不计新完成。
