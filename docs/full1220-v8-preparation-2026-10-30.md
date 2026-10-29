# V8：只读历史基线与完整范围声明

本轮新增两项离线准备：具体的 44 份历史 claim 基线，以及全新完整 V8 数据声明。原 1220×3 的 3660 对、7320 臂、11772 定义轮和 1851 边界保持；131 个基础窗口及其 caps 与 V7 相同。旧 566 臂不并入新完整样本，旧 scope 永久关闭。

## 历史基线

新私有 loader 在前后调用既有 concrete v6 loader，逐项核对 33+11 原 claim、前件及停止链，流式验证 source-pinned 审计 inventory 内的 6395 个明确原件。保留末笔 pending15/2688 和原 false flags。它绑定先前已独审的推理及原始字节，不将哈希或审计 JSON 变成新的质量证明。

独审发现新引用及读后元数据的硬链接检查缺口；一次集中修复后，新局部读取器检查前后 inode、类型、Nlink、大小等，旧 loader 和旧原件未改。新增负例先复现 10 项失败；最终作者 normal/race 各 65 项通过。窄复审无新增 P1/P2，报告 SHA `414bcc937e618972141e4cd618f0c86f47f6f67174b2ac6481268f1cef74d0a4`。

唯一新增历史 descriptor SHA 为 `2a78a0188ffd5072f75aeb51c9d9d54c5fdd46edc18a521571aa8aaccf38e1df`，不是金融 claim 或新预算批准。

## 纯 V8 声明

候选预算和重试声明分别使用独立字段与摘要，不冒用旧130批准。预算声明 SHA `ca7a9c43141a0fc416b24f019567a489935d8988db155fd0f56bc87f5014d725`，重试声明 SHA `6a2fb5869eed58f8ed2fee18b6a471ddbb264bffa56f8aabbc6b4eac95598d63`。模型90秒、JEV10秒保持原期限；逻辑上限58860/5886，候选 native 上限58880/5906。候选 HumanAuthorized、AllowPaid、ReadyForPaidRun 均为 false。

旧1–7版本 JSON 与 HEAD overlay 逐项比较一致；原 campaign 增加明确的 V8 提前拒绝，实际入口的版本门禁未放开。作者 normal/race 各130项通过；独审无 P1/P2，报告 SHA `daeb69a5e2eff8647b469c4f48946da10e8b5f5ff7ec0aca1ac7fb4578948a0c`。

## root 最终核验与后续

两项合并独立复测196项通过，0失败/跳过、stderr为空，包83.443秒；其中修复后真实固定原件核验63.36秒。七份源码 SHA 前后一致，日志 SHA `4369231c56087e31e517c6f8abff32377f5d36445541abc2d0a5de8bfd180455`。原RED、旧版actual与最终日志分别保留，不将旧日志当修复后的证据。

尚未实现真实 extra claim、全局20+20配额、owned native retry、payload/observer与新授权入口的完整贯通；完整源码回归与冻结也未完成。因此没有启动新评测、owner、真实claim或部署，累计仍3489模型/890JEV，44永久预留¥51.200203+$0.399529。

当前批准上限仍¥130+$1。候选完整新范围加最多40份额外预留、历史责任和风险为¥139.218667+$1.053354，拟议¥140+$1.1及新重试政策尚未批准。后续最终精确授权与技术声明分开，避免批准后再改源码常量。
