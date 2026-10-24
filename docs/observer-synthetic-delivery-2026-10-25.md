# 本机合成遥测零推理验证

导出诊断提交 `4d887382300a906ae6d1a33972f3fe4d4aca4e0d` 推送并读回后，使用该版 Observer 一次执行固定24条新建合成 trace，每条3个 span。结果为 Accepted72/Exported72/Failed0/Dropped0、24批导出、队列0、Shutdown成功；第一次只读查询即完整读回72项。随后第二次只读核对所有观测的唯一ID、trace绑定、三种名称、版本、evaluation环境、专用release和diagnostic_zero_inference标签，全部匹配；input/output均无正文。

该探针只读取显式私密evaluation配置并强制本机端点，进程内关闭内容采集、采样1，专用release为observer-synthetic-delivery-20261025。未修改配置文件或共享服务。最多24轮、命令内部90秒上限，每轮结束后ForceFlush，沿用生产默认2秒导出超时；若导出报错或Failed/Dropped增加即停止，不重发、不重试、不恢复旧输出。使用独占0600结果文件，逐轮同步保存；未触碰原测评trace、claim、客服或业务数据。

每轮顺序生成agent.turn与两个明确命名的合成span，无模型/JEV请求、usage或评分写入。原始探针、只读检查程序及绑定SHA均归档。初次检查只验证数量/名称/正文为空，最终检查额外验证ID和完整元数据；这两次均是只读查询，没有重复导出。

Flush时间最小11.598ms、中位35.565ms、最大273.547ms，只是本轮导出调用时间，不是用户端到端延迟。24次紧邻顺序flush不复现原重复队列运行时长、负载或完整任务，不能称故障永久修复、历史根因确认或真实任务收益。

7,978份既有私密证据SHA不变。当前运行客服仍50e3c8d/v5，未部署新Observer，未新增实际模型/JEV或claim，累计仍117模型/31JEV，原重复队列继续stopped，已消耗c7f14ee3 claim与完整永久预留不改。

证据见[验证结果](../eval/paired-pilots/2026-10-25-observer-synthetic-delivery/validation.json)。本次没有产品源码变动；执行源码与刚通过全Go18包及race的4d88738相同。独立证据审查无剩余P1/P2；初版/最终读回脚本SHA混淆已用分别归档与三阶段provenance修正，原执行记录保留。结果在归档review.json中记录，不把审查者源码阅读称作另一次真实执行。

下一步选择先离线准备带诊断的新独立重复队列范围和永久预算链，保留原停止队列全部事实；不补跑第17–24对、不释放旧预留。新实际付费范围仍需按既有项目约定冻结精确版本和单独授权，常规准备过程不逐项询问。
