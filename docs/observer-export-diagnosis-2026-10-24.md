# 观测导出失败：离线诊断结论

本轮排查确认了计数含义和历史证据缺口，尚未确认2026-10-24重复队列第16对导出失败的具体根因。没有改导出时限、重试策略、停止守卫、运行客服或共享服务配置；没有新增真实模型/JEV调用，也没有重放真实trace。

原执行器9ff31b1的processor只在ExportSpans返回nil时累计Exported；返回error时把该次调用的整个span批次计入Failed，并把底层错误替换成固定“telemetry export failed”。因此Failed3的单位是span，不是3个HTTP请求，也不能仅凭它证明丢失了3项数据。原始记录未保存失败HTTP请求数量、span关联、HTTP状态、响应解析或transport分类；无法事后从计数还原。

历史证据保持：Accepted232/Exported229/Failed3/Dropped0，原命令exit1、batch016failed和entry综合false。随后只读找到了232项观测、32trace/72轮正文和元数据精确吻合。服务端可读不能反向证明原回执成功，也不能把原stopped改成成功。17–24对仍未运行，原claim永久消耗，完整248/24预留不释放，实际累计仍117模型/31JEV。

故障时间窗为北京时间23:06:30–23:10:30（UTC15:06:30–15:10:30）。本机web/Postgres/ClickHouse在该窗口没有日志条目；worker记录4次notification-queue的30秒Redis socket timeout，其中2次早于测评开始。Redis在停止后约36秒记录一次成功后台保存。当前容器显示自前一日启动、运行中、OOMKilled=false。这些记录无法关联到具体OTLP请求；空日志和当前状态也不足以排除当时的延迟、网络或主机问题。不能把通知队列超时定为导出根因。

新增测试只连接5个临时literal-loopback接收器，使用现有真实Observer/OTLP exporter。每种恰好一次请求，接收端读完3条合成span：

| 接收端行为 | 客户端Exported / Failed | Flush、Shutdown |
| --- | --- | --- |
| 正常HTTP200空protobuf回执 | 3 / 0 | 成功 |
| 完整读体后等到客户端响应截止 | 0 / 3 | 失败 |
| 完整读体后HTTP503 | 0 / 3 | 失败 |
| 完整读体后关闭连接 | 0 / 3 | 失败 |
| 完整读体后HTTP200但protobuf响应非法 | 0 / 3 | 失败 |

四种失败均没有重试，失败会持续体现在后续Flush/Shutdown；包含合成私密标记的响应正文未出现在返回的固定错误中。接收器读体不是持久化证明。这些实验只证明多个原因可产生相同计数，不是在复现原故障。测试使用1秒时限，生产2秒时限保持原值。

要让下一次故障可诊断，需要在未来执行器保留有界、脱敏的请求结果：单调请求序号、span_count、耗时、HTTP状态（若有）及有限的transport/响应解析错误分类；不记录地址、凭据、header、响应正文或用户内容。当前底层错误已被丢弃，不能补填历史原因。不能以“后来读回完整”为条件忽略失败或自动恢复付费队列，也不能仅延长时限宣称修复。新的实际测评仍需独立冻结范围及明确批准。

本轮为诊断测试和档案交付，未实现或部署上述未来诊断字段。验证结果、日志摘要和来源SHA见 `eval/paired-pilots/2026-10-24-observer-export-diagnosis/`；原始服务日志只保存在私密 `.tmp/paired-live-private/observer-export-diagnosis-20261024/`。最终回归与独立复核以同目录validation/independent-review为准。

最终定向5种场景及标准全Go18包通过，taskbench286.275秒；独立审查无P1/P2，独立定向在指定MacOSX26.5 SDK及临时端口权限后通过。审查者首次默认SDK链接失败及随后沙箱端口拒绝均保留，非生产缺陷修复。7961份旧私密证据摘要未变，测试后隔离库0。累计仍117模型/31JEV，无新claim、收费测评或部署。
