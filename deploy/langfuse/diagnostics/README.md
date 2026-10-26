# OTLP 接收端阶段诊断（离线模块，尚未部署）

此模块为后续接收端诊断提供经过离线验证的契约。目前没有修改镜像、路由、compose、服务配置或启动器；现有接收进程没有加载它。它不修复历史超时，也不授权新付费测评。

使用 Node 内置 AsyncLocalStorage，无第三方依赖，默认关闭。明确 enabled:true 后，用 run(fn) 包住一个 OTLP 请求的异步处理，用 stage('s3_upload',fn) 和 stage('queue_add',fn) 包住原方法调用。请求对象、结果、错误消息、正文、URL、头和凭据不作为诊断输入。阶段之外和重叠/无法确定的工作标记为 other。

```js
const {createOtelStageDiagnostics}=require('./otel-stages.cjs');
const diagnostics=createOtelStageDiagnostics(); // 默认透传，没有计时器或日志
// 未来经独立镜像适配后，在对应调用点保留原this、参数和业务Promise：
// return diagnostics.run(() => originalHandler.apply(this,args));
// return diagnostics.stage('s3_upload',() => originalUpload.apply(this,args));
// return diagnostics.stage('queue_add',() => originalAdd.apply(this,args));
```

固定2000ms仅是观察阈值：未完成时输出pending及当时阶段，慢完成或失败再输出终态；快速成功不输出。不取消原操作，不设置业务超时，不增加请求或重试。诊断保留返回值、拒绝/抛出值；默认关闭时连Promise身份都保持。开启后跟踪原生Promise，其他值原样透传且按同步完成处理，不读取任意业务对象的then/toJSON等属性。它不声称诊断自定义thenable的后续执行。

每实例最多保存/输出8条记录，最多跟踪8个并发请求，每记录最多两个完成阶段汇总；超额记录计入omitted_records，新请求因并发或记录上限未跟踪计入skipped_requests。完成后的snapshot()返回深复制的脱敏快照。日志失败被忽略；观察计时器创建失败则直接执行原业务，不引入错误。原操作在2秒后继续运行时，pending不是取消或入库失败证明。快照序号仅为当前实例的本地序号，不是trace、租户或供应商请求ID。

now/schedule/cancel仅供离线测试注入；正式适配不从HTTP或模型输入接收这些函数，不接收自定义阈值、阶段名或输出字段。原生时钟采用performance.now，定时器unref且完成后取消。并发阶段按保守other记录，不能把同一阶段的累计耗时当无重叠墙钟时间。

验证：

```sh
node --test deploy/langfuse/diagnostics/otel-stages.test.cjs
```

后续仍须单独实现并验证当前固定镜像的准确调用点适配、启用/回退、请求与日志关联及接收进程中的实际效果。离线模块/隔离原实现的成功不等于现有服务已具备分段诊断，不替代全量回归或新付费范围授权。
