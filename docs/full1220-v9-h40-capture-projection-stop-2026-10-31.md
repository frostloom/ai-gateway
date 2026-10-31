# V9第40窗：离线投影停止与修复边界

前39窗已完整交付317对、634臂、912轮，1899项评分各发布一次并完整读回；5404条唯一观测的内容、工具、父链及路由metadata均核实。第40窗只列为采集和源码核验完成，不计入新的完整交付快照。完整1220×3＝3660对目标仍未完成。

## 停止发生在哪里

第40窗原首次评分操作在读取第七个trace后抛出ValueError。原操作目录、错误、publish-api-intent中的may_have_sent_POST=true和禁止重入标记全部保留。七份GET均为完整200响应；没有重取它们，也没有重新进入原发布操作。

固定发布程序先验证全部trace，再创建窗口batch、全部逻辑／score intents，最后逐项写sent、发送POST并记录ACK。独立只读核对发现：第40窗28个候选的batch不存在，112份候选intent／sent／ACK记录也全部不存在。这些源码顺序和持久证据支持本次尚未走到评分POST阶段，不将外层“可能已发”标记误当成实际发包证明，也不把该标记改成false。

## 精确根因

原Toolbox.Preview在调用业务预览前同步捕获Input(args)。change_plan的findPlan随后解析套餐名称，并在同一map中补入plan_id。响应trace同时保存原args和预览后的resolved_args；旧离线Go投影helper对所有phase都使用resolved_args。

第七份GET的tool.preview.change_plan input为调用时的name-only对象，投影却预期额外的plan_id=2，因而触发Captured tool content mismatch。该工具output一致；七份原GET的native、turn metadata和agent input/output叶校验均通过，前六份tool校验也通过。问题是离线期望值的参数时点，不能通过放宽比较器、删除多余字段或改写Langfuse原件解决。

## 当前采集与预算

采集累计322对、644臂、936轮；本范围1311次模型、322次JEV、1633个native全部settled，重试、unknown和pending均0。含历史累计4868次模型、1240次JEV尝试。87份永久claim含预领取41，预留¥63.891644＋$0.574839；新known配置用量¥1.150719＋$0.016452，均不代表供应商账单。总预算仍为¥200＋$4，本范围Parent为¥180＋$2.5，旧责任不释放。

原采集owner仍在H40 idle，scope未关闭、进程未重启、未发送41。冻结生产源码4ddd37ad、binary、manifest、scope、账本、原helper目录和catalog均不变。修复期间没有新增真实模型／JEV调用、金融claim或部署。

## 修复与恢复合同

新helper只修正可据原trace重建的调用时点参数：成功preview使用原调用参数，read／execute保持原resolved参数路径；失败、缺失或无法重建的证据继续failclosed。原content()、脱敏开关、shape边界和严格比较规则不变。

新test-only helper须在独立runtime真实构建、形成新的SHA及签名catalog，不能借旧catalog签名为新二进制背书。新consumer仍须原fresh source／actual factory及真实owned capture过程才能生成进程内proof，不从JSON恢复opaque。HashKey只经原绑定校验后的stdin传入。

原失败操作永久不重入。另一个精确冻结、独审的首次发布入口只承接尚未创建的原score IDs：保持原scope和registry，重核全空态，复用七份原完整GET，仅允许剩余三trace各一次首次GET。十个trace严格校验全部通过后，才按原batch→intents→sent-before-POST→ACK顺序首次发布。任何冲突或不确定仍只允许验证／hold，不允许重发。321双臂的原结构失败和语义正分抑制保持，也不能将下期PendingPlan2说成当期Plan1已改。

计划见[capture-input correction](superpowers/plans/2026-10-31-capture-input-correction.md)。离线模板修复已在main的3582b34ec提交，经独立审查通过；真实Go投影回归11项、相邻评分测试19项通过，旧helper原样保留。新模板SHA为98a26427ab04797c26c88356e88800d417362e215b5fbf9c39531b2de36c41d9。

补充consumer已在1a315d412提交并独审通过，19项新测试及121项相关回归通过。隔离构建的两个helper均accepted且cleanup确认；新catalog经限定sign-catalog调用独审、签名和真实验签通过，原catalog保持不变。

实际原工厂与受保护的延迟stdin投影已完成：第40窗原7份GET对应7个trace、15轮、66个observation全部严格匹配；预期仍为10个trace，未读取的3个保持pending，all_pass=false。第39窗原10个trace全部通过，未重新请求任何原GET。首次离线尝试因原issuer输出目录约束在工厂与HashKey前被拒，其原输出保留；后续独立目录遵守原路径限制，未放宽issuer。

首次评分恢复入口fe02c916b已实现，15项新测试及71项相关回归通过，仍待独审及完整变更冻结。当前没有新评分POST、Langfuse GET或第41窗命令。原H40保持idle，首次评分和读回闭合后再继续原完整评测，不扩大实验或预算。

| 证据 | SHA256 |
|---|---|
| 精确只读根因诊断 | 90d7bd02f6a6d54c023b6d74dad6328bafe14428ac1550c0b1d74a832623a95b |
| 第40窗原summary | 571f1c5553a554d8d52a6990909ba33d8ed064efaa60a87508ca99073e4ff127 |
| 第七份原GET body | 3621200334cc41a07b44adcc5a4a23a60e0af2adde5515a22951332b0ce799dc |
| 评分停止边界独审 | 3e127c2492b1d08e419f87918e60547c84e1ffd3c41b986a2b1be972149ee7c1 |
| 恢复边界设计独审 | ce08f4bd8671d562718dffe9e765526313305f86c5a874945c345d0a948aab22 |
| 前39窗交付及当前停态独审 | 13bcb70ccbfb6bf202f77e8e0f92a74e748ce67e3ba8df67b112e7de8ee961ef |
| 前39窗动态累计 | aabc5740ae6a3fc45a5d60f5edefea4e38887ba685efe94bc6dde547a0b74c8b |

补充工程证据：新catalog `2a5ed70eb9eb76b95a77d790aa69f0ae156a48686429636111041efd7a8a0cb8`；binding `561a5ebf8e59e4cea4ff5bece7c29160e2a1d4f77443ee2037a387f19cff0dd7`；真实离线回放结果 `1e9e1c39ef1f5893787e0f4ee306058a020135ffd54f606e09e6aac1988120b9`。这些是校验器交付证据，不是第40窗评分完成证明。
