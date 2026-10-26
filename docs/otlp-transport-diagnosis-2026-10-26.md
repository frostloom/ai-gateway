# OTLP超时：请求写出后未及时收到响应

本轮仅零推理诊断，没有模型/JEV调用、新claim、客服部署或旧span重发。
停止测评结果已经提交并推送main `c2cb584a213a7b5aec71264f4e685ecbfff6a286`；
其原始失败、claim及完整永久预留保持。实际累计仍119模型/31JEV，预留¥20.680704+$0.158592。

## 单次合成传输探针

同一显式evaluation配置、2秒导出、禁重试，创建一条全新合成span；不含对话正文。
仅在独立探针进程包装默认transport，拒绝非固定本机端点、非POST或第二次请求，
httptrace只记录事件名称/时间/成功布尔，不记录URL、头、凭据、正文或错误字符串。
原业务源码及服务配置未改。

新trace `07a72c23c8a2ae1a633864b868daaa00`：代理未选用，连接约1.415ms完成，
请求写入约1.597ms完成，约2001.838ms RoundTrip超时返回，未收到首响应字节或HTTP状态。
Accepted1/Exported0/Failed1/Dropped0、QueueDepth0；flush和shutdown失败，未重试。
这将本次探针的问题定位到请求写出后等待响应的区间；不是服务内部具体阻塞点的证明。

稍后只读查询新trace得到1项agent.turn观测，说明这次合成请求虽未及时ACK，后来可见。
不能将客户端超时一概解释为永久丢失，也不能把这个结果套用到原测评。
原测评trace第三次独立只读查询仍0项；前两次0项及原stopped/needs_review/Successfalse保留，
未发布评分，也未把新合成trace当原数据补齐。

## 接收路径对照

当前本机镜像为ai-gateway-langfuse-zh:4.58.0-local-v2，镜像ID另存档。
直接读取运行镜像编译代码：OTLP路由校验/解析后等待publishToOtelIngestionQueue；
该方法先await S3 uploadJson，再返回队列add的Promise，然后路由才能返回。
这里只说明当前代码顺序，不证明某项依赖是超时根因。

一次零span空OTLP请求在同一2秒时限内200返回，耗时127.579ms；
它跳过S3/队列发布，不是非空请求完整验收。
接收容器同网络新建客户端只读探针：Redis初始化及连接271.999ms，PING0.259ms；
S3 HeadBucket200、142.226ms。一个独立diagnostic前缀的新合成对象Put200、90.124ms，
仅删除该自建对象204、5.440ms；单次、2秒、SDK禁重试，未投递业务队列。
这些独立客户端的小负载对照不能替代运行服务的共享连接、上传和队列发布测量。

有界日志见worker的data-retention-processing-queue发生30秒Redis socket timeout，
但没有关联本次OTLP请求的证据；不得据此归因。容器healthy也不保证2秒摄取ACK。

## 尚待定位

未确认物理根因，未实施修复。下一步应分别测量真实接收进程中S3上传和队列发布的等待，
或建立隔离的同实现探针；优先补证据，不改2秒导出、失败停止或重试政策。
原测评不得续跑/重领；新的实际付费范围必须另行冻结并明确批准。

证据：`eval/paired-pilots/2026-10-26-otlp-transport/`；
私密源：`.tmp/paired-live-private/otlp-transport-20261026/`。
