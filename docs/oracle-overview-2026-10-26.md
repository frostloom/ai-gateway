# 配对Oracle同次总览复用与执行准备

同一次 `pairedStoreOracle.State` 对本人原来读取三次 `MeOverview`，控制账户一次；
现在本人和控制账户各一次。本人这次新读同时供余额快照、额度及独立权威数据使用。
不跨采集缓存；公开 `Snapshot` 和 `CaptureConversationAnswerAuthority` 仍各自新读。
订单、订阅、全部充值、审计、控制账户和账单查询及错误返回保留，仍非事务原子快照。

仅改taskbench三个实现文件及新增测试。客服15个非测试Go文件逐字节匹配已部署
50e3c8dab14b4b6cc25322fd340a274e6970200b，v5合同a95cacd不变，无部署或账务代码改动。

## 验证

原始正常场景阶段计时：runner22944.889ms，200次State累计12013.965ms，
120次journal写入555.088ms，建库1143.215ms。均为私密overlay诊断，非实际测评性能。
初次profiling子测试正则匹配到3项而非2项，原日志保留；之后正常子例精确匹配。
CPU/阻塞采样不用于确认历史主机物理根因或把并发等待相加为墙钟耗时。

最终测试对旧三文件源码overlay实测完整目录查询4次、期望2而RED；当前代码GREEN。
活跃订阅额度、公开API深比较、后续目录/额度/账单更新、错误身份、取消及本人/控制/
权威账单查询三类失败传播均验证。定向race退出0，10.161秒。新测试最初缺少账单模型
目录关联、故障注入参数位置错误均已修正，失败日志保留。独立最终审查无P1/P2。

正常88模型替身/24FakeJEV、纠正120/24，两条路径各24对48账户104轮。
208完整请求与185源码SHA绑定；独立汇总各24对eligible0，无独立真实质量结论。
官方tokenizer重新下载SHA一致，最大7311字节/1754序列化文本token，输出128、thinking disabled。
合成usage不作计费依据，本地文本token不保证供应商账单。

原600秒完整回归退出1：17包通过，taskbench600.560秒累计超时，155项顶级测试累计
595.4秒，未出现单项断言失败，停在既有首批队列测试。用户随后明确选择“整套测试20分钟（推荐）”。
最终完整命令为 `SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk go test ./... -count=1 -timeout=20m -json`：
18包退出0，taskbench424.339秒；214账户fixture16.12秒、原六案例验收17.39秒均实际通过。
新诊断16路径114.80秒、旧重复11路径85.04秒。原断言、各请求/SQL时限及测试调度保持。
相同源码在不同负载轮次耗时不同，不据此单独证明性能因果，不倒填600秒失败。

后续Go整包累计窗口按用户批准使用20分钟；真实 `paired-live` 队列仍5分钟。
该测试策略授权不是新付费范围批准，¥100+$1总预算不变。

## 执行边界

新真实范围仍为v5-repeated-export-diagnostic-20261025，原24对48臂104轮，最多248模型及24JEV；
保留七份完整前置claim及四项诊断来源绑定，失败停止，不补原17–24。
提交后按最终HEAD归档并冻结私密false授权/计费材料；最终执行器、binary及各SHA以
`.tmp/paired-live-private/regression-timing-20261026/final-binding-check.json` 为准。
公开材料human/allow_paid/ready仍false，不复用历史批准，不创建新claim。

DeepSeek/TypeSafe认证GET及本机Langfuse健康认证均200；TypeSafe版本ID无需出现在别名列表，
见[官方模型契约](https://docs.typesafe.ai/models)。保持deepseek-flash与jev-1.13.0。
当前[DeepSeek峰时公开费率](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/)和
[JEV公开费率](https://docs.typesafe.ai/models)核对后仍保留原保守声明预留，非账单保证。

本轮新增实际评测模型/JEV/claim/评分/部署均0，累计117/31，永久预留仍¥12.300288+$0.094080。
若未来独立批准领取新范围，新增¥8.380416+$0.064512，累计¥20.680704+$0.158592，
未分配¥79.319296+$0.841408；旧完整预留不释放。
全部旧证据和claim不改写，最终隔离库及SHA核验见同日oracle-overview档案validation.json。
