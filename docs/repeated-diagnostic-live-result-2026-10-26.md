# 独立重复诊断测评：第一臂导出超时停止

用户明确批准本轮一次后，从冻结执行器
`f0a329ca68888be767693a619b3b77d67c2e0b08` 实际执行
`v5-repeated-export-diagnostic-20261025` 一次。命令63.995361秒退出1，非外层300秒超时；
第1对model臂完成后，观测失败守卫停止，selective臂及其余23对未启动。
没有重跑、续跑、重领或补原17–24。原24对/48臂/104轮未完成，完整配对0，汇总eligible0。

实际2次DeepSeek模型尝试，3562输入/39输出，冻结配置计价7436microCNY（¥0.007436），
非供应商账单；JEV实际0。与历史117模型/31JEV合计119/31，不能把未运行的selective臂
当0成本成功任务，更不能计算两臂或总体收益。

## 原始失败证据

原observer计数Accepted5、Exported0、Failed5、Dropped0、QueueDepth0。
新增有界诊断记录4个失败批次，各1/1/2/1个span，耗时约2003/2003/2010/2004ms；
error_kind均deadline_exceeded，stage均round_trip，每批观测到1次HTTP round trip，
请求序号1–4；未观测HTTP状态。第一条transport_cause为deadline_exceeded，其余unknown。
Failed5是span数，不是5个HTTP请求。证据确认本次导出调用触发约2秒时限，不能据此确认
连接、服务处理、持久化的具体物理原因，更不能回填旧Failed3的历史根因。

两次独立只读查询原trace `0a03704880b8e04f1c89021e2faeea97`，当时均为0项观测，
无法完成Langfuse正文/元数据匹配。这不证明永久丢失；未重发旧span，也未发布评分。
7项确定性评分只作本地计划，未运行臂无trace；不伪造完整pair以发布独立语义分数。

原entry-outcome的cleanup_and_receipt_succeeded=false保留；另行只读pair/fixture隔离库0，
不将残留0改写为综合清理成功。

## 独立审查

唯一已完成model臂的3项原要求独立语义复核均passed：读取本人现金余额，回复
“当前现金余额为 ¥5000.00（账本余额与之一致）。”与现金及账本500000分一致，
没有虚假写入完成声明。原needs_review/Successfalse保持，批次仍failed，未运行臂未审。
独立停止/预算审计23项通过；不把单臂语义正确解释为配对或整队列成功。

## 永久领取与预算

新claim原始SHA为
`8c63988dbd31b09cb523c7e0b067f5978ad0ee91f5736f0fc581752544161e6a`，已永久消耗。
248模型/¥8.380416＋24JEV/$0.064512的完整声明预留全部保留；累计永久预留
¥20.680704＋$0.158592，已批¥100＋$1未分配¥79.319296＋$0.841408。
已开始batch前缀租约仅6模型/1JEV（¥0.202752/$0.002688），实际模型尝试预留¥0.067584；
这些与完整scope永久预留、配置计价是不同口径，不释放差额或重用本次批准。

旧七claim及旧私密/公开证据9277项SHA不变，本轮无客服部署。
Go整包测试20分钟政策仍有效，真实测评整命令仍5分钟；测试时限批准不扩展付费授权。

下一步仅零真实推理排查本次OTLP失败，优先确认实际传输与本机接收路径。
不修改2秒导出、失败停止或自动重试政策，不重发旧trace。任何新付费范围须重新冻结并明确批准。

公开档案：`eval/paired-pilots/2026-10-26-repeated-diagnostic-live-result/`。
原始私密证据：`.tmp/paired-live-private/regression-timing-20261026/live-run/`及同目录审查/只读回执。
