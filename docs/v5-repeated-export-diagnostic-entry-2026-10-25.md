# 带导出诊断的独立重复入口

本轮离线实现 `paired-live -repeated-diagnostic`，固定身份 `v5-repeated-export-diagnostic-20261025`。授权 `v5-repeated-export-diagnostic-authorization.v1`，计费 `v5-repeated-export-diagnostic-billing-attestation.v1`，回执 `authorized-v5-repeated-export-diagnostic.v1`，claim `paired-live-repeated-export-diagnostic-claim.v1`。公开模板 `human_authorized`、`allow_paid`、`ready_for_paid_run` 仍为 false。本轮没有真实模型 POST、真实 JEV、新 claim 或客服部署。累计实际调用仍是 117 次模型 / 31 次 JEV。原 `v5-repeated-cohort-20261024` 停在第 16 对，17–24 对不恢复、不重领。

## 调度与诊断绑定

客服仍是 `50e3c8dab14b4b6cc25322fd340a274e6970200b`，合同 `a95cacd041830012619a0f4d7fd8208c184aa9e8d45b7ee05afaa239a30e1e8f`。24 对 / 48 臂 / 104 轮，248 模型 / 24 JEV，整命令 5 分钟，失败即停，不重试、不续跑。`IsRepeated()` 继续作为事实错误和导出失败的停止策略；`IsRepeatedDiagnostic()` 只负责新入口，并且排在历史 `-repeated` 之前。历史 runner 和六前置 claim 路径不接受新 schema。

检查器拒绝把 observer 修订 `4d887382300a906ae6d1a33972f3fe4d4aca4e0d` 填进 `execution_revision`。回执在原 JEV 诊断字段之外，带上已经提交的四项绑定：observer `4d887382300a906ae6d1a33972f3fe4d4aca4e0d`、observer 源码 SHA `16121832bb7f1850e699aab4b005bc9e3755016dc430002a490406352c6f3ba9`、合成交付 provenance `08f6badd79c44bf2d9648ae0698f2c5a324144bbede791e4de5606017d331409`、原 stopped manifest `41b2560c7f4230cad1c8b22df4123544f2687a6c2a275a3c2fe7bcf66cf781e8`。这四项不是执行版本。历史回执的零值绑定继续 omitempty，不会多出 `observer_revision`。

## 永久 claim 与整数预留

创建新 claim 前必须同时匹配七份已消耗原文：原确认、诊断重测、取消、补充、退款、六例 cohort，以及原重复 claim `c7f14ee3b310cec23cf71b7dc004fe73b29ec5e8efa4b6c4167eb91159c8dd58`。缺一或摘要变化时拒绝，并且不创建新文件。新路径仍是 O_EXCL、0600、文件和父目录 fsync。第二次输出或版本不覆盖。

当前永久预留不变：¥12.300288 + $0.094080（12300288 / 94080 micro）。若将来只批准并领取这一范围，新增 ¥8.380416 + $0.064512，累计 ¥20.680704 + $0.158592（20680704 / 158592），100+1 未分配 ¥79.319296 + $0.841408（79319296 / 841408）。旧六前置路径的累计数字保持 12300288 / 94080，不写入 `prior_repeated_claim_sha256`。仓库里的新 claim 文件不存在。

整范围预留和已开始 batch 的前缀租约分字段保存。提前停止时前缀小于 248/24，整范围数字仍是 248/24/8380416/64512。

## 本地执行

真实 Handler、隔离 MySQL fixture、官方目标地址上的替身传输和 FakeJEV：正常 88 模型 / 24 FakeJEV / 104 轮 / 48 个独立账户和 trace；纠正 120 模型 / 24 FakeJEV。两路都是 `checkpoint_pending`，`needs_review` 且 `Success=false`。合成 usage 按配置费率计价，报告写明不是供应商账单。

导出失败打在现金追问那一臂结束之后：503 记 `http_status`，完整读体后的 200 截断 protobuf 记 `response_decode`。回执保留最多 8 条诊断，span 数等于原来的 `Failed`，`Dropped=0`。该对 Finished 1 / Unrun 1，后续对没有目录。同一目录再跑不会增加模型请求或导出请求。

两种臂顺序下，提供方、JEV EOF、缺 usage、事实不符、缺本轮读取和清理失败都会停住后一臂。回执文件已存在时，24 对跑完后是 `stopped/receipt_write`。这些计数是替身，不是真实调用。

## 费率与执行包

重新下载官方 tokenizer ZIP，SHA `e7310d1dafe0a86d8a5629fe78a7c763760f651db9b8682718a1781dcd6fe495`；其中 `tokenizer.json` SHA `89085f12ef79460ac5f66d1119325ddfc694b4ab209d80bbd81d35f081dc9614`，与既有重复入口证据一致。公开价格页上 `deepseek-flash` 高峰缓存未命中输入 2 元/百万、输出 8 元/百万；TypeSafe 页上 `jev-1.13.0` 仍是输入 $0.042/百万、64k。没有发送模型 POST。公开页不是账户账单，计费模板的费率/token/失败收费标志保持 false。客服非测试 Go 相对 `50e3c8d` 无差异，因此不重抓那 208 份已归档请求；它们的最大体积仍是 7311 字节、1754 个本地文本 token，输出上限 128，thinking disabled。

公开 false 模板、七份前置 claim、预算整数和请求包络在 `eval/paired-pilots/2026-10-25-repeated-diagnostic-entry/`。执行版本没有冻结成某一次付费 SHA。付费仍须单独批准这一精确范围，不能沿用旧 cohort 批准，也不再询问 100+1 总预算。

本环境 Hindsight `127.0.0.1:8888` 连接被拒绝，这条记忆只写在本文件和 `AGENTS.md`，不能当成已经读回的库记录。
