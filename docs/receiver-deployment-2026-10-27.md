# Langfuse接收端诊断部署与零推理验收

按2026-10-27用户最新连续自主推进授权，本机Langfuse web已切换到验证过的v4镜像
`sha256:677a486f0519c772b5db1217b028d4b2c8a9db7cd960468aadf6f71e944f532f`，诊断开关启动值1。
覆盖此前“候选未部署”状态，但不表示原超时根因已确认或付费测评已完成。

使用原compose.yml加独立compose.diagnostics.yml（不再叠加旧zh build字段），
预先渲染完整web有效配置，核对只有镜像和诊断开关变化，无build字段。
执行web-only、no-deps/no-build/pull never，健康200；其余27个容器的ID、镜像与启动时间均未改变。
web挂载和原.env完整SHA不变，数据库/worker/Redis/S3/ClickHouse卷保留，无模型服务/客服部署。
原镜像d04cd20a完整ID仍在，私密回退overlay渲染后与原有效配置相等，未实际回退或删除镜像/卷。
compose提示既有卷非当前Compose创建的warning保留，没有为消除warning重建或改卷。

已读回运行容器完整补丁/模块SHA、0664及1001:1001，配置启用为1。
单独node读取的snapshot属于新进程，不冒称读取了web进程内快照；本轮健康快速成功无诊断日志符合合同。
诊断仍只覆盖认证后的callback、S3上传、queue add，认证/路由/模块加载不在覆盖范围。

首条新无正文合成trace约390.984ms收到首字节/HTTP200，Accepted1/Exported1/Failed0，flush/shutdown成功。
随后独立新trace等待45秒，间隔2/3/2秒导出1/1/2/1span，4次HTTP200，flush约
131.127/53.680/160.393/41.429ms，Accepted5/Exported5/Failed0/Dropped0/QueueDepth0。
两trace共6项观测的名称、专用release、唯一ID和无正文均只读核对通过；没有重发任何旧span或发布评分。
健康、项目认证/项目数据与登录入口均200，项目数据与部署前一致。
这些只证明限定零推理验收成功，不是长期负载稳定、历史数据修复或模型质量证明。

本轮新增真实模型/JEV/claim均0，实际累计119/31，永久预留仍¥20.680704+$0.158592。
用户今晚明确批准模型请求等、自主连续推进、不逐步确认：后续可在已批¥100+$1内先冻结
新的独立scope、版本、次数、预算链，再按本次预授权执行，不重复逐scope问答。
此项覆盖旧AGENTS/记忆中的逐范围再询问流程，不放宽技术门禁、失败即停、旧claim不续不重领/不释放预留。
用户原话及解释留在0600私密授权材料；公开仅保存其SHA。剩余总额¥79.319296+$0.841408。
下一步立即实现/验收新的独立24对重复测评范围，保留原六例、四block顺序平衡和5分钟整命令上限，
不复用已消耗的20261025诊断scope。按新预授权自主完成必要回归、冻结、执行与独立复核。

公开证据：eval/paired-pilots/2026-10-27-receiver-deployment/；
私密备份、回退、原始日志及新用户授权：.tmp/paired-live-private/receiver-deploy-20261027/。
