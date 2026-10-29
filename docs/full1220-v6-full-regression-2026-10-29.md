# v6选定源码完整回归与推送

选定提交`4ccec396f628a15050376eb1b34946e39d79a34a`已通过原配置`go test ./... -count=1 -timeout=20m -json`，1082.673秒退出0。49包中23包通过、26包无测试；2995项测试通过，3项既有条件跳过，失败0、stderr为0。新v6原始停止证明、授权basis和版本守卫测试均逐项JSON显示通过，未跳过。

归档2166个跟踪文件、444个Go文件均与Git blob一致，客户15个非测试Go文件与50e版本逐字节一致。归档tree为`64b6a1b0caa8497e74dc9c56fd670fe1aa4d01ed`，source-map摘要为`b3c414296d1fd170e7b1a3d91fecdfff2d30ca5c6263a6643dec606372326c08`。测试前后源码摘要不变；本项目9个服务按原ID、镜像、配置和挂载恢复健康，临时caffeinate已退出，未操作其他项目。

3项跳过为TestCashLoad、TestCashProfileRowTiming和TestLiveJevSemanticsDiagnostic。此前定向race中的链接器警告仍保留，不用本轮stderr为空抹去历史限制。Python完整回归321项/15项PowerShell或Git-Bash条件跳过对应4b07生产代码；随后8bf仅测试及固定数据变动，在无私密文件的干净副本中39项通过、0跳过。各自范围分别记录，不混称一次不同提交的整套运行。

4c已非强制推送main并通过SSH ls-remote读回确认。新release又完成一次零推理合成trace：2项观测、10项正文/ID/release/环境核对全部通过，无重发、模型调用或评分发布。此证据仅支持本次传输验收，不能证明长期稳定或模型质量。

当前总预算仍是已批准的¥130＋$1。旧32份永久责任¥42.961430＋$0.344493、实际2292模型/585JEV均保持；新实际owner、模型/JEV及financial claim新增0。原1220三轮3660对/7320臂/11772定义轮目标仍未完成，1851边界另列；没有以旧84对或离线测试替代。

下一关是私密运行校验器独立复核、最终manifest/请求包络/签名/授权/预算/封存与owner绑定。未冻结前不执行新实际scope；旧scope5不续跑、不重领、不释放责任。[代码阶段记录](full1220-v6-offline-integration-2026-10-29.md)、[实施判断](full1220-v6-implementation-rulings-2026-10-29.md)。
