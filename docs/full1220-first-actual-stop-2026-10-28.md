# 1220首次完整范围实际入口：首窗技术停止

原1220主任务尚未完成。原计划三轮交错3660对、7320臂、11772定义轮，1851边界另列；本次仅首个model臂发生一次供应商请求，0臂finished、0完整配对，7319臂未尝试、11771定义轮未运行。两臂知识配置仍off，知识专项100题和20题草案均不计入主结果。

## 冻结、实际执行和停止原因

执行源码为c1c446e991eedcb5c5e1a544e5e762981d8d2571，manifest SHA为2d6ccad655fb19fb0a945b326a5a80eedf54d0b6a9bb15d220b097127568193e。2000归档文件匹配提交Git blob及SHA；冻结全Go原命令退出0（23测试包通过、26无测试包skip），原Kafka条件skip保留，另以原Kafka测试非skip通过补齐。不是将skip改写为通过。

最初构建的`-trimpath`使Go1.25.12省略BuildInfo中的`-ldflags`；去掉该标志后，第二次只读检查仍因归档无自身Git元数据、继承父仓库子目录前缀而取得空树。两份拒绝结果保留。补齐独立detached Git证明元数据与索引后，原2000源码、manifest和main均未改；同一manifest重新签名的binary-v2通过实际只读check。签名binary SHA为0711b014fc42e2186cc8be54beb8ad5d51353b72e44caafa44ca9266ac2aedfd。

按既有自主预授权，单owner启动后显式执行window1一次，退出1并closed。首个adv-001/answer_complete的model请求收到HTTP200、完整506字节响应；原生usage为基础输入232、缓存读取1536、缓存创建0、输出37，另带`service_tier: "standard"`。归一输入1768，未超冻结包络。执行器usage白名单未允许该元数据，确定性返回`unsupported JSON field`，传输层按原规则停止为`protocol_failure`。这是本执行器的协议兼容缺陷，不能归因供应商HTTP失败、模型别名、token越界或模型质量。

DeepSeek的[Anthropic兼容说明](https://api-docs.deepseek.com/guides/anthropic_api/)提供接口背景；此次响应字段存在及具体拒绝点由保留的原始响应与源码直接证明，不以请求参数兼容说明代替响应实证。

## 资金与失败分母

新claim SHA为52bbf6e7edf8b66099141db5ac7c6782e45b8afc3bcd9145ea1fd51f936eca48，claim永久消耗，整额预算预留¥0.414586＋$0.004079保留，不释放、不重领。后130窗未领取，不能把全部131窗候选cap算作已预留。历史累计实际尝试144模型／37JEV，永久预留¥29.475706＋$0.227183，未分配¥70.524294＋$0.772817。

原ledger只有config、intent、halt、close，没有settle；known0、pending模型hold74752µCNY必须保留。按原生usage和冻结全部输入峰价只读重算为¥0.003832，非供应商账单，也不回写原ledger。不得因拿到usage便将技术停止改成成功或把原hold释放。

首个模型响应仅提出get_balance；协议拒绝使Handler在工具处理前返回500，没有实际余额工具执行、退款preview或退款写入。保留快照除采集时间外一致，但原final_oracle错误、cleanup=false、observer=false不能据此改成成功。原environment_cleanup=true；另一次独立只读隔离库核验为0。

所有计划任务进入失败/未运行分母。quality_negative_arms=0不表示模型质量通过；空回复及未交付Handler的供应商中间响应不能作为完整任务语义成功证据，不发布通过评分。

## Langfuse及离线修正

原回执观测快照Accepted3/Exported0/Failed0/Queue1保留。后续只读trace 26f8da2c5d2ee6e7b284b94e7517bc27找到3条唯一observation：llm.attempt、agent.turn、evaluation.task。运行版本、题目、scope和agent.turn的原用户输入及空回复读回一致。Langfuse中llm.attempt的input/output为null；本地506字节原生响应已保留，不能称完整供应商正文已进入Langfuse。首次旧API路径404、正确v2端点首次10秒读超时均保留；没有重发trace或发布评分。

最小修正仅允许model usage中可选的精确`service_tier: "standard"`，其他类型、其他tier、未知字段及JEV携带此字段仍拒绝。token归一化、价格、上限、固定供应商、失败停止和旧证据不改。新增回归先观察两项实际RED，再验证parser、一次transport结算、原字节保留及持久化账本；协议/传输/账本176条测试记录通过，0失败、0跳过。独立复核无P1/P2。随后按原命令`go test ./... -count=1 -timeout=20m -json`验证当前工作区，退出0、875.340553秒，23测试包通过、27无测试包skip；仅原三个opt-in测试跳过，无失败，Kafka原E2E本次非skip通过，stderr为空。Go源码测试前后SHA一致。这是当前工作区验证，包含工作区额外的无测试报告包，不冒称下一提交的精确归档冻结已通过。

下一次实际执行必须采用新版本及包含本次已消耗claim的独立历史预算链，重新冻结整个原1220三轮范围，不能只续7319臂。原131窗cap若不变，新全领后累计¥97.897368＋$0.760967，余¥2.102632＋$0.239033；扣除模型单未知额外风险¥2.023424后仅余¥0.079208。旧pending的完整响应按冻结费率责任3832µCNY≤旧hold74752≤旧cap414586，已由旧预留覆盖；新基线必须验证这条不等式及原始绑定，无法验证就拒绝新范围，不能机械忽略pending。新执行仍顺序进行，新增未界定请求最多一个，未知即全局停止。该余量不能用于知识补充、抽样或重试，也不保证全58860最大尝试或供应商账单。实施步骤见[独立范围计划](superpowers/plans/2026-10-28-full1220-poststop-baseline.md)。

回归后，原九个本项目容器全部running/healthy，ID、镜像、Config、Mounts与回归前核对一致；Langfuse health与认证projects GET均200，未改变镜像/配置/卷或其他项目。公开摘要及私密证据SHA见[证据清单](evidence/full1220-first-actual-stop-2026-10-28.json)。本轮后续验证零新增模型/JEV/claim/评分。
