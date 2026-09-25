# Go 速览：本项目用到的语法与并发原语

> 面向「Go 基础薄弱的自己」写。每一节都锚定到本项目真实代码——读完你不仅能看懂
> `ai-gateway`，还能在面试里回答「你这句代码为什么这样写」。
> 引用路径以仓库根目录为准。

## 0. 怎么用这份文档

先扫一遍 1~5，把「概念」看懂；再去打开对应文件看「代码」，按「为什么」核对你的理解；
最后过一遍第 6 节的面试速答题。这份文档不教你写 Go，教你**读 Go + 讲 Go**。

---

## 1. 语言基础速览（本项目出现的）

### 1.1 package / module / import

- 一个目录一个 `package`，目录里的文件共享包内符号（大小写决定可见性：小写=包内私有，大写=导出）。
- 本项目的模块名是 `github.com/frostloom/ai-gateway`（[go.mod](../../go.mod)），所以内部包 import 长这样：

```go
// internal/gateway/handler/chat.go:25
import "github.com/frostloom/ai-gateway/internal/gateway/client"
```

- `internal/` 目录是 Go 的「包私有」边界：只有父目录能 import 它。项目把三服务的业务代码都锁在
  `internal/` 下，外部无法 import——这是组织多服务单仓库的常用手法。

**为什么单 module？** 项目按「面试可讲」设计：一个 go.mod 管理 5 个 cmd 服务 + internal 库，
比 5 个 module 简单，面试也容易讲。真实大公司才拆多 module。

### 1.2 变量、零值、短声明 `:=`

```go
var total int64          // 零值 0（Go 没有「未初始化」）
n := rand.Intn(total)    // 短声明，类型推导
total += p.Weight
```

- 每个类型有**零值**：int=0，string=""，slice/map=nil，struct 各字段递归零值。
- `:=` 声明并赋值，`=` 只赋值。`var` 和 `:=` 等价，风格偏好而已。

### 1.3 函数、多返回值、error 惯例

Go 没有异常，错误是**普通返回值**：

```go
prov, err := r.Route(c.Request.Context(), req.Model)   // chat.go:84
if err != nil {
    abort(c, http.StatusServiceUnavailable, "routing unavailable")
    return
}
```

- 惯例：函数最后一个返回值是 `error`，调用方必须处理。
- 返回值命名可省略；`(resp, err)` 组合很常见。
- **写库/写 Redis 的返回要检查**，但「读失败不影响主流程」时用 `_` 吞掉并注明原因
  （如 [chat.go:188](chat.go) 的 `_ = json.Unmarshal(body, &pr)`——解析失败按 0 计，不致命）。

### 1.4 struct + 方法：值接收者 vs 指针接收者

```go
type Provider struct {           // registry.go:25
    ID      uint64
    Name    string
    BaseURL string
    Weight  int
}

func (r *Registry) Route(model string) (*Provider, error) { ... }  // 指针接收者
```

- **指针接收者** `(r *Registry)`：方法能改 r 的字段（比如改 `b.state`）。Registry 是「状态机容器」，
  必须指针。
- **值接收者** `(p Provider)`：方法拿到副本，改不动原对象。只读的方法用值/指针都行。
- 选哪个：**要不要修改 + 对象大不大**。要改用指针；对象大（复制贵）也常用指针。
- 方法集是面试必考点：值接收者的方法，值/指针都能调；指针接收者的方法，只有指针能调。

### 1.5 接口：鸭子类型（没有 implements）

```go
type BillingServiceClient interface {   // 由 protoc 生成（billing_grpc.pb.go）
    Reserve(ctx, in, ...) (*ReserveResponse, error)
    ...
}
```

- Go 接口是**隐式满足**的：类型只要方法齐全，就自动满足接口，不需要 `implements` 关键字。
- 这带来一个极有用的面试句式：**「这里用接口，是为了能 mock/替换实现」**。比如 reconciler
  持有 `billingv1.BillingServiceClient` 接口，集成测试可以塞一个假的 client，不真连 billing。

### 1.6 常量与 iota

```go
const (                              // registry.go:32
    breakerClosed breakerState = iota
    breakerOpen
    breakerHalfOpen
)
```

- `iota` 从 0 递增：Closed=0, Open=1, HalfOpen=2。紧凑地定义一组枚举。

### 1.7 切片与 map：别被「引用类型」骗了

```go
filtered := make([]Provider, 0, len(avail))   // registry.go:98
for _, p := range avail {
    if !exclude[p.ID] {
        filtered = append(filtered, p)
    }
}
tried := map[uint64]bool{}                    // chat.go:135
```

- 切片/ map/函数/通道是**引用类型**：传递时复制的是「头部结构」（指向底层数组的指针），
  底层数据共享。**但 append 可能换底层数组**，所以「传 slice 到函数里 append，外面看不到」。
- map 的**零值是 nil，不能直接写**：`var m map[uint64]bool; m[1] = true` 会 panic。
  必须 `make` 初始化——本项目都是 `make(map[...]...)`。
- `range` 遍历 map 顺序随机，这是语言设计（面试可能会问，答：保证可预期性会牺牲并发与哈希性能）。

### 1.8 defer 陷阱：不是「最后执行」那么简单

```go
func (r *Registry) markProbe(pick *Provider) {   // registry.go:115
    r.mu.Lock()
    defer r.mu.Unlock()                          // 函数返回时解锁，LIFO 顺序
    ...
}
```

- `defer` 在**函数返回时**执行，参数在**defer 声明时**就求值了（不是执行时）——这是最常见的坑：

```go
// 坑：打印的是循环结束时的 i（声明时求值的是地址）
for i := 0; i < 3; i++ {
    defer fmt.Println(i)   // 会打印 3 3 3
}
// 对：defer func(i int){...}(i) 手动传值
```

- defer 的三大用途：解锁（本项目的 `mu.Lock/defer Unlock`）、关资源（`defer upResp.Body.Close()`）、
  记录耗时。LIFO（后声明先执行）在嵌套 defer 时注意。

---

## 2. 并发：本项目的核心考点

Go 的并发模型是「goroutine + channel + 同步原语」。本项目实际只用了最朴素的几样——
**面试时「讲清为什么选它」比「会多少花样」值钱**。

### 2.1 goroutine：超轻量「协程」

```go
for i := 0; i < *concurrency; i++ {   // scripts/loadtest/main.go:84
    wg.Add(1)
    go func() {                        // 100 个并发压测 worker
        defer wg.Done()
        for time.Now().Before(done) { ... }
    }()
}
```

- `go f()` 起一个 goroutine，**不等它返回**。栈初始仅几 KB（线程是 MB 级），百万级没问题。
- 语言层面有 GMP 调度器：M=线程，P=处理器，G=goroutine。面试被问「goroutine 和线程区别」
  就答：更小、更快、更易创建，Go runtime 自己调度（Go 1.14 后是抢占式调度，可协作式让出）。
- **goroutine 泄漏**是生产事故高发点：起 goroutine 却没人能结束它。常见解法是 context 取消。
  本项目的 worker 用 `time.Now().Before(done)` 自限时长，主 goroutine 用 `wg.Wait()` 等齐，
  **没有泄漏**。

### 2.2 sync.WaitGroup：等所有 goroutine 收工

```go
var wg sync.WaitGroup
wg.Add(1)        // 计数 +1（必须在 go 之前）
go func() { defer wg.Done(); ... }()   // 完成时 Done()（计数 -1）
wg.Wait()        // 阻塞直到计数归 0
```

- 三个方法：`Add`/`Done`/`Wait`。**Add 必须在 goroutine 启动前调**，否则可能 Wait 已经返回了
  才 Add，造成「等了个寂寞」。
- 适用场景：分治后汇总（压测收集、批量请求）。**不适合**：生产消费者管道——那是 channel 的活。

### 2.3 sync.Mutex：保护共享状态（本项目最重要的并发原语）

```go
type Registry struct {
    ...
    mu  sync.Mutex
    bk  map[uint64]*breaker    // 熔断状态：多 goroutine 读写
}

func (r *Registry) Report(providerID uint64, ok bool) {   // registry.go:127
    r.mu.Lock()
    defer r.mu.Unlock()        // 整段临界区一把锁
    ...
}
```

- 并发**写**共享数据必须加锁。gateway 是 HTTP 服务，多个请求 goroutine 同时调 `Report`，
  不加锁 `b.consecutiveFail++` 会丢计数（read-modify-write 竞态）。
- **锁粒度**：本项目用**一把大锁**保护整个 registry 状态。并发不高（路由选择），简单正确优先。
  面试问「会不会太粗」→ 答：路由是 O(候选数) 的极短临界区，且核心路径在 Redis/DB 那侧，
  registry 内存在这里不是瓶颈；真要优化再拆 RWMutex 或分片锁。
- `sync.RWMutex`：读多写少时用。本项目的 `mu` 只有 `Report`（写）和 `available`/`snapshot`（读），
  读多，理论上可换 RWMutex。**但**：写操作里会调 DB（`persistFail`），持写锁调 DB 是危险的反模式，
  所以干脆全用 Mutex 图简单。

### 2.4 sync/atomic：无锁的计数器

```go
atomic.AddInt64(&reqs, 1)            // scripts/loadtest/main.go:75
atomic.LoadInt64(&reqs)
```

- 原子操作是对 int 的 CAS，无锁、性能最好。适合「只加不减」的计数器。
- **不是银弹**：负载是高（如 `statusCnt map[int]int64`），原子救不了 map——map 本身要锁
  （本项目用 `statusMu` 保护 statusCnt，见 loadtest 注释：Load-then-Store 会丢计数）。
- 面试点：什么时候用 atomic、什么时候用 Mutex？答：单个整数计数用 atomic；复合结构/读改写
  需要一致性用 Mutex。

### 2.5 context：请求生命周期 + 超时/取消（本项目最关键的坑之一）

```go
// 转发用的「请求 ctx」：客户端断连即取消
uReq, err := http.NewRequestWithContext(c.Request.Context(), ...)   // chat.go:148

// 结算用的「后台 ctx」：绝不能复用请求 ctx！
settleCtx, settleCancel := client.BackgroundCtx(10 * time.Second)   // chat.go:333
defer settleCancel()
```

- `context.Context` 是贯穿一次请求的「取消/超时/元数据」载体。
- **请求 ctx**：`c.Request.Context()` 随客户端断连自动取消——用于转发上游（该取消时就得取消）。
- **后台 ctx**：`context.Background()` + `WithTimeout(10s)`——用于结算。为什么？
  客户端断连了（请求 ctx 已取消），**计费还要继续**：上游跑完了，该扣钱照扣。
  复用请求 ctx 的话，客户端一断连，Settle 的 gRPC 调用被 cancel，表现就是
  「请求看起来成功了但永远不扣费」——静默丢账。这是项目文档里反复强调的坑。
- `context.WithTimeout` 返回 `(ctx, cancel)`，**cancel 要 defer 调**，否则定时器泄漏。

### 2.6 channel：本项目没用，但必考

本项目没用 channel（goroutine 之间靠 Mutex 共享状态 + 显式传参）。但面试必问，补上标准句式：

```go
ch := make(chan int, 5)       // 有缓冲：写 5 个不阻塞
ch <- 1                       // 发送，无缓冲/缓冲满时阻塞直到有人接收
v := <-ch                     // 接收，空时阻塞
close(ch)                     // 只有发送方能 close
```

- 心智模型：**「不要通过共享内存通信，要通过通信共享内存」**（Go 谚语）。
  但实际工程里，「多个 worker 同时改一个 map」用 Mutex 比用 channel 分发消息更常见、更好读。
  面试答：channel 适合生产-消费者/流水线/信号通知（如 `<-done` 收工）；共享数据结构用 Mutex。
- 常见坑：向已 close 的 channel 发送 panic；读已 close 的 channel 得到零值（用 `v, ok := <-ch` 判断）。

---

## 3. 网络与 IO

### 3.1 http.Client 必须复用（压测血泪教训）

```go
var (                                    // chat.go:55
    upstreamClient = &http.Client{
        Timeout:   2 * time.Minute,
        Transport: &http.Transport{MaxIdleConns: 256, MaxIdleConnsPerHost: 64},
    }
    streamClient = &http.Client{         // 流式无总超时，靠请求 ctx 取消
        Transport: &http.Transport{MaxIdleConns: 256, MaxIdleConnsPerHost: 64},
    }
)
```

- **为什么必须包级共享？** `http.Client` 并发安全，连接池在 `Transport` 里。
  `http.Client{}` 是零值，**每次 new 一个新 Transport = 一个新连接池**。
  压测第一轮每请求 new Client，100 并发 × 30s 疯狂建连，把 Windows 临时端口（TIME_WAIT）
  打爆，报 `Only one usage of each socket address`。复用后修复。
- 面试金句：**「http.Client 是长生命周期的并发安全对象，必须复用；Timeout 设到 Client 上，
  连接池大小配在 Transport 上。」**
- 为什么流式 client 不设 Timeout？流式长连接可能挂几十分钟，总超时会误杀。取消靠请求 ctx。

### 3.2 SSE 流式与 bufio（M3）

```go
reader := bufio.NewReader(upResp.Body)        // chat.go:258
line, err := reader.ReadBytes('\n')           // 逐行读
...
flusher.Flush()                               // 每帧 Flush，客户端才能看到增量
```

- `bufio` 缓冲读，`ReadBytes('\n')` 按行。SSE 协议就是「以 \n 分行的 data: 帧」。
- `http.Flusher`：gin 的 `c.Writer` 实现了它，`Flush()` 把缓冲立刻写给客户端。
  **不 Flush 的 SSE = 客户端一直等到超时，看不到任何增量**。
- 面试点：SSE 和 WebSocket 的区别（单向 vs 双向；SSE 基于 HTTP，有自动重连/Last-Event-ID，
  WebSocket 全双工）。

### 3.3 JSON：struct tag 映射

```go
type usage struct {                                  // chat.go:47
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
}
```

- tag 决定字段名映射，否则用字段名原样匹配（大小写不敏感）。
- 本项目所有对外结构都写 tag；大小写敏感时（如 `request_id`）必须 tag。

---

## 4. 这个项目的几个 Go 设计模式

### 4.1 依赖注入：New(...) 把依赖塞进来

```go
func New(db *gorm.DB, log *slog.Logger) *Registry {   // registry.go:70
    return &Registry{ db: db, log: log, ... }
}
```

- 每个服务把 DB/Redis/日志/RPC client 通过 `New` 注入，不在内部自行 `gorm.Open`。
  好处：单元测试可以传 `db=nil`（registry 单测不连库，见 [circuit_test.go](../../internal/router/circuit_test.go)）。
- 面试答「依赖注入是什么」：让对象不自己 new 依赖，而是由外部提供——解耦 + 可测试。

### 4.2 无超时循环：对账常驻

```go
for {                                                       // reconciler.go:116
    if s, err := r.SweepOnce(ctx); err != nil { ... }
    select {
    case <-ctx.Done():       // 收到退出信号
        return
    case <-time.After(interval):   // 到点跑下一轮
    }
}
```

- `select` 多路等待：谁先到执行谁。`ctx.Done()` 用于优雅退出（SIGTERM）。
- 这个模式 = **定时任务的 Go 写法**：`time.After` 每轮产生一个新 channel，select 阻塞到超时。

### 4.3 错误只处理一次：不吞、不重复打日志

- 本项目惯例：业务入口处理错误（`abort(...)`）；辅助操作（退款）失败只记日志不中断
  （[chat.go:368](chat.go) 的 `reverseWithBackground` 注释写明「退款失败交给 M4 对账」）。
- 面试金句：**错误要「向上传递一次，处理一次」，日志打在最能定位问题的层。**

---

## 5. 构建与质量

### 5.1 gofmt / go vet / go build

```bash
gofmt -l .     # 列出未格式化的文件（本项目已全部格式化）
go vet ./...   # 静态检查（发现 unreachable code、格式化串错配等）
go build ./... # 编译全部
go test -race ./internal/...   # 带竞态检测跑单测
```

- `-race` 是面试加分项：竞态检测器在测试运行时插入检测代码，发现并发数据竞争就报。
  本项目并发测试（reserve/settle 幂等）就该用 `-race` 跑。

### 5.2 环境变量配置（config.Getenv）

```go
port := config.Getenv("GATEWAY_PORT", "8080")   // cmd/gateway/main.go:25
```

- 用环境变量覆盖默认值：docker、脚本、本地开发同一份二进制。
- 本项目配置走 `internal/pkg/config` 封装的 `Getenv`，键值直读，无配置文件——面试可说
  「12-factor：配置进环境变量」。

---

## 6. 面试速答（Go 部分，含本项目对照）

### Q1 值类型和引用类型区别？
值类型（int/string/bool/struct/数组）赋值复制整份；引用类型（slice/map/channel/函数/指针）
复制「头部」，底层数据共享。**坑**：slice 的 `append` 可能换底层数组；map 零值不能写。
本项目对照：`Provider` struct 按值传（复制小对象）；`tried` map 到处传（共享）。

### Q2 切片扩容机制？
`append` 超容量时按「翻倍（小容量）/ 1.25 倍（大容量）」换新数组。所以 `append` 后
底层地址可能变。面试常考：`a := make([]int, 3, 5)` 的 `len=3 cap=5`，append 到 5 内不换数组。

### Q3 defer 的执行顺序？
**LIFO**（后声明先执行）。参数在声明时求值。三用途：解锁、关资源、记耗时。
本项目：`defer r.mu.Unlock()`、`defer upResp.Body.Close()`、`defer cancel()`。

### Q4 goroutine 和线程区别？
goroutine 用户态、几 KB 栈、由 Go runtime 调度；线程内核态、MB 栈、OS 调度。
Go 1.14+ 抢占式调度。创建/切换成本低几个数量级。

### Q5 channel 无缓冲和有缓冲的区别？
无缓冲：发送方阻塞到接收方拿走，强同步（管道）。有缓冲：缓冲满才阻塞，松耦合。
`close` 后读得零值，发送 panic。

### Q6 怎么防止 goroutine 泄漏？
1) goroutine 要有明确的结束条件；2) 用 context 取消；3) 用 `select` 监听 `ctx.Done()`。
反例：goroutine 里 `for` 死循环等 channel 但没人 close。本项目 worker 用时间自限 + WaitGroup。

### Q7 Mutex 和 atomic 怎么选？
单整数计数用 atomic（更快）；复合结构或需要「读-改-写」一致性用 Mutex。
本项目：请求计数 atomic；熔断状态（多个字段要一起变）Mutex。

### Q8 context 的正确用法？为什么结算不用请求 ctx？
context 传递取消/超时/元数据，沿调用链传递。请求 ctx 随客户端断连取消，用于转发；
结算这类「请求结束后还要完成的工作」必须用 `context.Background()` + `WithTimeout`，
否则客户端一断连 Settle 被 cancel，账就悄悄丢了。**这是本项目踩过并修掉的真实 bug。**

### Q9 什么时候用 channel、什么时候用 Mutex？
生产-消费者/流水线/事件通知用 channel；「多个 goroutine 修改同一份数据」用 Mutex。
心智模型：channel 传递数据的所有权，Mutex 保护数据不被并发改。

### Q10 panic 和 error 的区别？
error 是业务错误，用返回值显式处理；panic 是「不该发生」的程序错误（数组越界、nil 解引用），
会崩掉整个进程。恢复用 `recover`，只能在内层的 deferred 函数里生效。
本项目全程 error 返回值，零 panic——**生产代码的风格是「拒绝 panic 主义」**。

### Q11 怎么排查并发 bug？
`go test -race` 竞态检测器 + 压力测试（本项目压测就是在 100 并发下暴露了 statusCnt
计数丢失、连接池风暴两个真实问题）。面试加分：**「我不但写了测试，还压测验证了不变量。」**

---

## 7. 自测清单（口述抽查）

1. 为什么 `http.Client` 要复用？（连接池在 Transport；每 new 一个就是新连接池→连接风暴）
2. 为什么结算用后台 ctx 不用请求 ctx？（客户端断连取消请求 ctx → settle 静默丢失）
3. `defer r.mu.Unlock()` 的 defer 参数什么时候求值？（声明时，不是返回时——但 Unlock 无参数，无所谓）
4. map 为什么必须 `make`？（nil map 写入 panic）
5. `-race` 有什么用？（检测并发数据竞争）
6. slice 传进函数里 append，外面能看到吗？（不一定——append 超 cap 换底层数组）
