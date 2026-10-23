# JEV 传输故障排查与脱敏诊断

本轮接续[充值确认真实测评失败](recharge-confirm-live-result-2026-10-23.md)，只读连接诊断及本地故障注入，没有重发推理。原 `recharge-confirm-20261023` 的 failed、永久 claim、预算预留和历史证据保留；本轮新增真实模型/JEV 均为 0。

## 已查明与尚未知

原 JEV 请求在 3183.576584 ms 后只留下 `kind=transport`，没有 HTTP 状态、响应 usage、底层错误类别或分阶段时序。实际客户端总时限为 10 秒，TCP/TLS 超时各 10 秒；不能仅因 3.18 秒就认定触发了这些超时，也不能判断请求是否送达、是否执行或收费。

使用当前评测私密配置的同一个显式代理，通过 `cmd/paired-live` 原 `explicitTransport` 做有限的官方 `GET /v1/models` 检查。使用 Go overlay 只附加诊断测试，不改生产传输配置；只记录 TCP/TLS、连接复用、首字节时序，不记录认证头或响应正文。

| 模式 | 连接 | 结果 | 总耗时 |
| --- | --- | --- | --- |
| HTTP/2 | 冷连接 | TLS 1.3、ALPN h2、HTTP200 | 576.960 ms |
| HTTP/2 | 复用 | HTTP200 | 306.042 ms |
| HTTP/1.1 | 冷连接 | TLS 1.3、HTTP200 | 527.905 ms |
| HTTP/1.1 | 复用 | HTTP200 | 296.922 ms |

这排除了“当前这四次只读请求始终不可达”的假设，**没有复现或解释原 POST 推理失败**，也不能证明 POST、大请求或后续连接一定正常。原故障根因仍未确认；没有依据切换代理、关闭 HTTP/2、放宽超时或自动重试。本轮没有更改这些设置，也未重启或部署客服。

## 诊断缺口的最小改动

原客户端为避免泄露地址和密钥，主动丢弃原始传输错误。保留这一安全边界，给 `jev.Failure` 增加可选 `transport_cause`：仅通过 `errors.Is/As` 检查类型和标准错误码，输出有限枚举 `dns`、`connection_refused`、`connection_reset`、`broken_pipe`、`tls_certificate`、`eof`、`unexpected_eof`、`timeout`、`canceled` 或 `unknown`。无法可靠分类时保持 unknown，不按错误正文猜测。

只在 HTTP Do 失败时增加类别，现有 Kind、Error 文案、HTTP 状态和 `upstream_attempted=nil` 语义保持；本地发送前取消仍是明确未尝试。该字段能区分后续一部分传输原因，不是完整网络阶段追踪，也不能定位错误发生在代理还是远端。任何类别都不能自动推出免收费或允许重试；旧记录不回填类别。

新字段经过原 FailureDetails、agent 遥测及 JEV 预算日志保留；重开日志仍计入失败尝试，次数和费用预留不释放。没有增加自动重试、改变提供方 payload、错误停止策略或费率验证开关。

## 验证范围

先写失败回归，观察到类别缺失的 RED，再实现最小分类。测试覆盖包装后的 DNS、标准连接错误、TLS 证书、EOF、超时、取消和未知错误，验证日志不含合成私密正文、恰好一次尝试、送达仍未知。真实本地 HTTP/TLS 服务器补充证书拒绝、断连、代理端口拒绝与请求超时；预算 guard 测试覆盖日志类别持久化以及重开后不重试。

新增超时夹具曾因等待未读 POST 请求的服务端 context 而在清理阶段阻塞；已用本地测试栈定位，改为测试显式释放 handler，带 30 秒运行时限的 JEV 全包验证通过。该测试问题与供应商故障无关。

独立审查未发现阻断问题。Windows 的原生 Winsock 错误可能保守归入 unknown，不宣称所有平台细分类完整覆盖。后续新真实测评仍要独立范围、构建绑定和批准；本轮只改善排障证据，不声称已经修复历史故障或完成新的真实验证。

完整 `go test ./... -count=1` 在进程级 MacOSX26.5.sdk 下 18 个测试包通过，另查隔离库残留 0；131 份历史私密证据及原 claim 摘要未变。脱敏时序、验证和源码绑定见 `eval/paired-pilots/2026-10-23-jev-transport-diagnosis/`，原日志仅放被忽略的 `.tmp/paired-live-private/jev-transport-diagnosis-20261023/`。
