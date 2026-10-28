# 实际窗口失败传输诊断与旧epoch封闭

当前源码为下一独立执行器增加失败才出现的有限诊断：原生响应descriptor可选transport_failure记录typed cause、最远观测phase、毫秒耗时和trace/连接复用/请求写完/首字节布尔。不记录地址、URL、头、证书、错误字符串或回调参数；无trace不能推定没发送，写完或收到首字节也不能证明供应商处理或收费。

标准httptrace在每个受保护请求各自创建状态，锁保护并发回调，最远阶段不被晚到的失败拨号回调倒退；快照和payload记录复制值。错误分类复用JEV现有typed分类，正文/headers/URL/模型参数未改变。正常响应及旧JSON不出现新字段；HTTP状态、读取前缀、unknown hold、halt及禁止下一发送规则保留。SDK10秒、窗口300秒及各原超时未调整，没有新增重试或预热推理。

端到端deadline反例原来仅保存round_trip，缺cause/phase，断言RED后GREEN；late callback、伪成功诊断、无trace却声称已写字段也先RED再GREEN。独立审查发现P2：超限与close错误同时出现时新增close诊断会使原超限前缀保存失败。原组合反例RED后只在主code为response_close时添加close诊断，保留原response_bound优先级及1048577字节前缀，最终race245项/两包通过，无失败/跳过，taskbench5.950秒，stderr0。

联合race还发现旧JEV EOF测试裸计数跨HTTP handler与测试goroutine竞争，改为atomic.Add/Load，原一次请求断言保留；失败日志不删除，不把它归因新业务逻辑。初次边界测试另有沙箱Go-cache读取拒绝，保留后按原配置定向执行，不计为断言RED。

只做一次逻辑模型目录GET，原冻结本机JEV HTTPS路由、10秒client限制、无显式重试；当前返回200，httptrace观测phase=read_response、615毫秒、连接未复用且已观测请求写完和首字节。没有模型/JEV推理或claim。该GET只证明当前可达及诊断回调工作，不证明第六窗故障修复；旧JEV物理原因仍unknown，旧响应/held/false字段没有回填。

另已封闭当前源码中的旧actual v1/v2 epoch：Open在settings/root/claim前要求v3版本，consumed-v2反例RED→GREEN，入口/旧只读等61项race通过。v3十六claim基线与实际ready尚未实现接线，因此当前源码不能用旧十claim基线开启新实际范围。原归档v2材料仍只读可核验，原已消耗owner不重开。

此处只完成定向/race及独立审查单元；新的精确归档完整Go20分钟门槛仍待执行，不能称新实际范围已冻结或已执行。父预算为用户新批准¥105+$1，永久¥33.395934+$0.259511不变，当前历史809模型/204JEV尝试。本单元新增实际模型/JEV/claim/部署0。机器证据见`docs/evidence/full1220-transport-diagnostics-2026-10-28.json`。

计划与剩余任务见[传输诊断计划](superpowers/plans/2026-10-28-window-transport-diagnostics.md)。后续先完成新十六claim停止证明、v3完整范围/实际入口、精确归档回归与签名门禁，再按已有自主授权执行；不拿本次GET或替身测试代替真实完整3660对。
