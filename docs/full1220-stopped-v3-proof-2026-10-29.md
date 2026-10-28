# V3停止后的25份claim具体只读证明

新增五份私有Go实现从原16份具体证明连接到V3新增9份，重新推导25claim永久38888170µCNY/308003µUSD、未分配66111830/691997、实际1791模型/445JEV。旧held及原第一笔已知liability沿用原证明，不做结算或释放。只读证明不提供provider、claim、actual Open或恢复能力，新类型不满足旧双根v3执行接口。

读取57固定顶层原件，包括实际owner exit1/relaunchfalse；再逐项读取482记录、1223原生请求及完整HTTP200响应，核对每笔intent、settle、response SHA、重新解析的usage、各窗claim永久额及summary链。前8窗成功与第9窗budget_exhausted、原false/audit-only状态全部保留，不把前端blocked模型调用算为实际发送。

严格campaign/records库存拒绝额外claim10、checkpoint9、窗口10、子record或native sidecar缺失/改动；三个历史根不得交叉、别名或被新根覆盖。descriptor与前置具体证明前后重读，内存base变化亦拒绝；仅对旧有.lock打开只读fd，不创建旧锁或修改旧文件。

断言RED后，正常83项/1.897秒、实际私密overlay159项/19.705秒、组合race243项/174.575秒及最终unit race83项/3.986秒通过，均0失败/跳过/stderr。root固定五文件SHA再运行83项通过，7.446秒；独立固定SDK26.5定向退出0，2.286秒，无确认P1/P2。9174份旧证据前后SHA一致。早期日志包装的zsh只读变量错误保留，不当作整命令通过；空expectedSHA原guard本来就拒绝，直接GREEN没有伪称修复。

本单元0新模型/JEV/财务claim/评分/部署，没有运行全Go或DB。它只完成新历史基线，尚无新完整scope或付费ready；后续仍需新计价版本、完整Handler/native验证、精确归档全Go20m及新签名冻结。原完整3660对目标保持active。

[机器证据](evidence/full1220-stopped-v3-proof-2026-10-29.json)包含五文件SHA、真实derived值、日志与旧原件清单摘要。下一单元见[缓存分拆结算计划](superpowers/plans/2026-10-29-cache-split-accounting.md)。
