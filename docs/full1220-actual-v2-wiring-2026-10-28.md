# 完整1220实际入口v2：停止基线接线

v2实际ready、Open及逐窗verifyInputs已接入具体文件型停止证明，campaign仍在首claim及后续领取前独立重验十claim基线。当前仅接线与离线/替身验证完成，精确归档全Go20分钟、最终签名只读ready和新实际执行尚未完成。原1220三轮3660对、7320臂、11772定义轮和1851边界另列不变，两臂知识仍off。

manifest新增omitempty的baseline_evidence，绑定scope.v2的SHA、停止描述文件路径、原历史目录、十claim映射、新旧root隔离及3832µCNY旧责任。ready私有快照保存loader返回的具体证明；JSON不能生成它。Check只接受新实际v2版本；原已归档v1执行器及原始v1材料仍保留，新字段不改变旧JSON序列化和原停止loader的解码。

Open在凭据、网络及新目录前重验停止快照，再通过原完整ready门禁和健康预检；创建campaign使用newWindowCampaignV2。每个窗口的输入校验重新加载同一证据，不只核对原九claim。原pending、技术失败、永久预留及原始negative字段不修改，原300秒、2秒observer和无自动重试规则不变。

验证证据见`docs/evidence/full1220-actual-v2-wiring-2026-10-28.json`：

- manifest和Open检查缺失先出现断言RED，再GREEN；binding-red是未定义新函数的编译失败，只保留日志，不算断言RED。
- 定向原账本/协议/传输/停止材料/v2 campaign/实际入口联合race通过339项，两个包pass，taskbench85.747秒，stderr空。
- 原14文件与九claim只读loader及v2快照复验通过；仅篡改临时descriptor副本后，Open和下一窗口输入校验拒绝，未创建新session/campaign目录，包1.851秒。
- 新v2 collector使用真实Handler、隔离数据库、本机模型替身/FakeJEV；普通及standard各4/1，技术失败不推进、不创建第二claim。非法tier/缺usage各1/0，JEV EOF前有正常模型两次后2/1停止；observer503只尝试首臂、0 finished，允许该臂正常1–2次模型请求后停止。不能统称所有故障只有一次请求。
- cleanup是关闭sql.DB后直接调用原清理方法的单元故障，0模型/0JEV；不是完整runWindow清理故障回放。原collector兼容单独通过，不能把其日志算成新增v2用例。全部新v2用例race日志为actual-v2-handler-v2-only-race，不能用前一个仅选择旧四例的日志替代。
- 独立源码审查未确认P1/P2，未声称已完成完整有效ready/Open归档验收或实际供应商质量验证。

此次新增真实模型/JEV/actual claim均0，历史累计144/37、永久¥29.475706+$0.227183不变。测试私有v2 helper沿用假费率、非真实customer绑定、local_stub及内存历史证明，只服务替身测试，无法产生生产ready。

下一步从本单元提交生成新的完整归档，运行原配置全Go20分钟及所需fixture，再冻结同一完整范围的版本、价格/原生包络、证据SHA和独立roots。原首次实际停止scope不重开、不续跑、不重领。
