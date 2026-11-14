# Go 后端面试专项：Gin、gRPC 与项目实战

面向准备 Go 后端面试、需要从基础补起的读者。先理解概念，再追项目代码，最后脱稿回答问题。本文基于当前工作区；项目旧里程碑和 `.proto` 中部分计费注释描述的是旧模式，计费现状以[当前架构指南](current-2026-11-11.md)和实际代码为准。

## 1. 先建立整体画面

```text
客户端  -- HTTP / JSON / SSE -->  gateway（Gin）
                                    │
                   gRPC / Protobuf  │  HTTP
                                    ▼   ▼
                              billing   router → 模型上游
                            Redis / MySQL
```

- **Gin**：Go 的 HTTP Web 框架。在这个项目里，gateway 使用 Gin 注册路由、执行中间件、读取请求和返回响应。
- **gRPC**：远程过程调用框架。gateway 调用 billing 的鉴权、预扣、结算等方法时，通过生成的客户端发起 gRPC 请求。
- **Protobuf**：用 `.proto` 定义消息和服务接口，并生成 Go 类型；gRPC 常用它编码消息。Protobuf 是数据描述与编码格式，gRPC 负责远程调用，两者不是同一个东西。
- **HTTP/SSE**：面向外部客户端和模型上游；模型输出可能逐步返回。项目中 gateway 到 router 走 HTTP；不要说项目所有服务间调用都走 gRPC。

先看 [gateway 路由入口](../../cmd/gateway/main.go)、[billing 服务契约](../../internal/proto/billing/v1/billing.proto)、[gRPC 客户端](../../internal/gateway/client/billing.go) 和 [billing 启动入口](../../cmd/billing/main.go)。

## 2. Gin：面试要会讲什么

### 2.1 从一个请求看 Gin

项目使用 `gin.New()` 创建引擎，注册 Recovery 和观测等全局中间件；`/v1` 路由组再依次注册 API Key 鉴权、幂等、限流，并绑定 `POST /chat/completions`。入口见 [cmd/gateway/main.go](../../cmd/gateway/main.go)。

请求到达时可以这样追：

1. Gin 根据方法和路径匹配路由。
2. 路由对应的中间件链执行：API Key 校验、生成或处理请求 ID、租户限流等。
3. 鉴权中间件把验证得到的 `tenant_id`、`api_key_id` 放入 `gin.Context`。
4. [Chat handler](../../internal/gateway/handler/chat.go) 读取这些值、解析请求体、选择 provider、调用 billing 预扣并转发请求。
5. 返回 HTTP 响应，或通过 SSE 持续输出模型内容。

**追问：`gin.Context` 和 `context.Context` 有什么区别？** 前者是 Gin 的 HTTP 请求处理对象，封装请求、响应、参数及中间件数据；后者是 Go 跨 API 传递取消、截止时间和请求范围信息的接口。本项目从 `c.Request.Context()` 取得标准 context，传给 gRPC 和 HTTP 等下游操作。

### 2.2 中间件和 `Next` / `Abort`

中间件适合放认证、日志、指标、限流等多个接口共用的逻辑。它是请求链的一环，既可以在后续 handler 前执行，也可以在后续 handler 返回后执行收尾逻辑。

```go
// 示意：项目实际鉴权更复杂
func Example() gin.HandlerFunc {
    return func(c *gin.Context) {
        // 请求进入时
        c.Next() // 运行链上后续处理
        // 后续处理返回后，可记录状态码和耗时
    }
}
```

鉴权失败时，[APIKey 中间件](../../internal/gateway/middleware/apikey.go)调用 `AbortWithStatusJSON` 并 `return`，阻止后续 handler 执行并返回 401。`Abort` 负责阻止后续链，不会自动让当前 Go 函数停止，因此仍要 `return`。鉴权成功后，项目写入租户信息并调用 `c.Next()`。

**追问：中间件顺序为什么重要？** 例如租户限流需要可信的租户 ID，鉴权应先把它放入上下文；若先限流就不能直接依赖之后才建立的身份。具体行为仍要按代码和路由组核对。

### 2.3 Gin 常见追问

- **`gin.New()` 和 `gin.Default()`？** `New` 创建空引擎，按需注册中间件；`Default` 默认加入 Logger 和 Recovery。本项目使用 `New` 并显式注册所需中间件。
- **`ShouldBindJSON` 和 `BindJSON`？** 前者返回绑定错误由调用方决定响应；后者会在绑定失败时自动设置 400 等响应行为。项目 `Chat` 不是用这两个方法，而是读取受限大小的请求体并自行解析。
- **`panic` 和普通 `error`？** 业务失败通常返回错误并显式处理；Recovery 用于捕获 handler 链中的 panic、避免单个请求导致进程崩溃，不替代业务错误处理。
- **异步 goroutine 能直接长期使用 `*gin.Context` 吗？** 不应假定请求结束后它仍可安全使用：Gin 会复用上下文。确需异步处理时，复制必要的不可变数据并明确超时和生命周期；即使使用 `Copy()`，也不能把原响应写入当作请求结束后仍有效。
- **SSE 有什么不同？** 普通响应通常一次性写出；SSE 持续向客户端发送数据，需要处理刷新、断连和上游中途失败；已经发出的内容无法撤回。

## 3. gRPC：从零到能讲项目

### 3.1 gRPC 是什么？

RPC 可以理解为：调用方像调用一个方法一样，请远端进程执行操作，网络通信、编码和解码由框架配合生成代码完成。它仍然是网络调用：可能超时、断连、重复或返回未知结果，不能假设与本地函数一样可靠。

gRPC 通常基于 HTTP/2，支持多路复用、流量控制以及一元和流式 RPC；常用 Protobuf 定义契约和编码。HTTP/2 是传输协议，不等于 TLS；是否加密要看连接配置。gRPC-Web 和浏览器场景有额外限制。

### 3.2 项目中的一次 gRPC 调用

1. [billing.proto](../../internal/proto/billing/v1/billing.proto) 定义 `BillingService` 和 `ReserveRequest` 等消息，例如 `Reserve(ReserveRequest) returns (ReserveResponse)`。这是**一元 RPC**：一个请求、一个响应。
2. 根据 proto 生成的 Go 客户端和服务端代码提供类型与方法约束。
3. gateway 的 [Billing 客户端](../../internal/gateway/client/billing.go)持有连接和 `BillingServiceClient`，通过 `cli.Reserve(ctx, req)` 调用。
4. billing 在 [main.go](../../cmd/billing/main.go) 创建 `grpc.NewServer`、安装一元拦截器、注册服务实现并监听端口；业务实现见 [service.go](../../internal/billing/server/service.go)。
5. gRPC 拦截器读取 metadata 中的内部控制面令牌；校验失败返回 `Unauthenticated`，成功才调用 handler，详见 [controlplane/token.go](../../internal/pkg/controlplane/token.go)。

项目当前 `controlplane.Dial` 使用 `insecure.NewCredentials()`：**连接本身没有启用 gRPC 传输层 TLS**。metadata 令牌用于身份校验，并不提供传输加密；真实部署还须结合网络隔离和上层加密配置讨论，不能把“有 token”说成“数据已加密”。

### 3.3 一元、流式和 SSE

| 方式 | 请求和响应 | 本项目例子 |
|---|---|---|
| 一元 gRPC | 一个请求、一个响应 | gateway → billing 的 `Reserve` |
| 服务端流式 gRPC | 一个请求、多个响应 | gRPC 能力；不要误说本项目 SSE 就是它 |
| 客户端/双向流式 gRPC | 多个请求、一个或多个响应 | gRPC 能力，需看具体服务是否使用 |
| HTTP SSE | 服务端在 HTTP 响应中持续发送事件 | gateway 向客户端中继模型输出 |

### 3.4 Deadline、取消与重试

项目 API Key 鉴权对 billing 调用设置 2 秒超时；`Chat` 预扣调用设置 3 秒超时，具体见相应代码。`context.WithTimeout` 返回派生 context 和 `cancel`，用完应调用 `cancel` 释放相关资源；上游请求取消会传播到使用其派生 context 的下游调用。

但结算或冲正需要认真处理客户端断连：项目提供独立的限时 background context，避免直接继承已取消的请求 context。它不是“永不失败”的保证，也不应无期限地把所有任务放到后台。

**超时不等于服务端一定没执行。** 调用方超时可能发生在服务端已完成业务但响应未到达之后；重试需要请求 ID、幂等和状态查询或恢复机制。尤其不能因 gRPC 返回 deadline exceeded 就断言余额没有变化。

### 3.5 Status code 和业务返回码

传输或服务错误可以通过 `status.Error(codes.Xxx, ...)` 表达，例如认证失败、参数无效或服务不可用。业务结果也可能在响应消息里使用枚举，例如 `ReserveCode_RESERVE_INSUFFICIENT`；它和 gRPC status 是不同层。gateway 的 `Chat` 根据错误和响应码分别映射 HTTP 状态，参见 [handler](../../internal/gateway/handler/chat.go)。

### 3.6 高频追问

- **为什么用 Protobuf？** 有清晰的 schema 和生成代码，二进制编码通常较紧凑；可比较 JSON 的可读性与跨语言调试便利性。性能要看数据和场景，不能保证总比 JSON 快多少。
- **字段编号可以随便重用吗？** 不能。Protobuf 二进制消息依赖字段编号，删除字段后应保留编号、避免为新含义复用；类型修改也须考虑兼容性。
- **连接每次请求都新建吗？** 本项目 `Billing` 持有 `*grpc.ClientConn` 并复用，关闭时调用 `Close`。`grpc.NewClient` 创建客户端连接对象不意味着此刻已验证网络可达；真正调用仍可能失败。
- **gRPC 一定比 HTTP 快吗？** 不能一概而论。HTTP/2、多路复用和 Protobuf 可能有优势，但性能由负载、连接、序列化、网络和服务端处理共同决定，应实测。
- **metadata 与请求消息有什么不同？** metadata 是 RPC 的附加头信息，常用于认证和追踪；业务字段应有明确契约。本项目令牌和部分计费契约信息通过 metadata 传递。
- **拦截器与 Gin 中间件的相同点？** 都用于包装调用链，实现认证、日志等横切逻辑；一个作用于 gRPC RPC，另一个作用于 Gin HTTP 请求。

## 4. Go 后端还要补的基础

| 知识 | 需要会解释 | 在项目中看哪里 |
|---|---|---|
| goroutine 和 channel | goroutine 是并发执行单元；channel 用于通信和同步；退出条件要明确 | [ledger_batch.go](../../internal/billing/store/ledger_batch.go) 的后台循环 |
| mutex 和 race | 共享可变状态需要同步；`go test -race` 能发现运行中触发的数据竞争，但不能证明绝无竞争 | 同文件的 `sync.Mutex`、批次 map |
| context | deadline、cancel、传播，和不要把它当通用参数包 | [APIKey](../../internal/gateway/middleware/apikey.go)、[gRPC 客户端](../../internal/gateway/client/billing.go) |
| interface 和 error | 隐式实现；接口让业务依赖抽象便于替身测试；区分业务错误和系统错误 | [gateway handler](../../internal/gateway/handler/chat.go)、`internal/gateway/client` |
| Redis 和 Lua | 单次脚本原子执行；不等于 Redis 和 MySQL 跨系统事务 | [计费 store](../../internal/billing/store/mysql.go) 的 `reserveBatched` |
| MySQL 事务与索引 | 事务原子性、唯一索引的作用及其边界；区分隔离级别和锁 | [批量写账](../../internal/billing/store/ledger_batch.go)、[schema](../../scripts/schema.sql) |
| 幂等与故障恢复 | 同一请求重复执行产生同一业务效果，设计必须覆盖并发、超时和重放 | request ID、guard、账单状态及 [当前架构指南](current-2026-11-11.md) |

[Go 基础速览](go-primer.md)适合补语法；旧文档的具体计费和代码行号以当前源码为准。

## 5. 一天冲刺计划

| 时间 | 学习内容 | 检查标准 |
|---|---|---|
| 1 小时 | Go 基础：接口、错误、指针、goroutine、mutex、context | 能解释 context 取消、数据竞争和接口隐式实现 |
| 1.5 小时 | Gin 路由、中间件和请求生命周期 | 画出 `/v1/chat/completions` 的认证、限流与 handler 链 |
| 2 小时 | gRPC、Protobuf、metadata、拦截器、超时 | 能沿 proto → client → server 讲完一次 Reserve |
| 1 小时 | Redis、MySQL、预扣与批量落账 | 能解释并发扣款和 MySQL 落后时的风险 |
| 30 分钟 | 脱稿模拟 | 回答下方问题，不看答案讲清边界 |

时间只有两小时：先读第 1 节，重点学 2.2、3.2–3.5，再练习下列问题。

## 6. 自测：不看答案讲出来

1. Gin 中间件的 `Next` 和 `Abort` 有什么区别？为什么鉴权必须在依赖租户 ID 的限流之前？
2. `gin.Context`、标准 `context.Context` 与 gRPC metadata 分别承载什么？
3. 画出 gateway 调用 billing `Reserve` 的全过程，proto、生成代码、client、server 和 interceptor 各做什么？
4. gRPC 和 Protobuf 的关系是什么？HTTP/2 是否自动代表 TLS？
5. 调用超时后，为什么不能肯定服务端没有扣款？如何设计重试？
6. Redis Lua 保证了什么，不能保证什么？MySQL 5 秒批量刷盘意味着什么？
7. 为什么客户端断连后结算仍可能需要继续？如何避免后台任务无限运行？
8. gateway 对外 SSE 与 gateway 到 billing 的 gRPC 一元调用有什么不同？
9. 读 [Chat](../../internal/gateway/handler/chat.go)：为什么先选路再预扣？没有可用 provider 时会不会扣款？
10. 结合自己真实经历讲一个问题：触发条件、原因、你做了什么、怎么验证、剩余限制。
