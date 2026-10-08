# 2026-10-08 客服优化实验原始证据

本目录保存本轮候选客服版本的失败探针、JEV 判定诊断及 fake 功能回归。V1 / V2 历史指标仍以 `eval/baselines/2026-09-29-v1-v2` 为准，此处没有修改其冻结档。

18 份实验原文件按原目录结构保存为 `.gz`。`manifest.json` 记录原始字节 SHA256、归档 SHA256、字节数；gzip 使用 level 9、mtime 0、空文件名。所有原始文件保留在本地，没有覆盖或删除。完整 `agent.log` 包含错误上下文，未截取到只剩有利结果。只读取明确白名单的报告目录及三份诊断 JSON，不扫描其他日志或 `.env`。常见凭证模式检查命中 0，未打印秘密；模式检查本身不证明覆盖全部秘密格式。

## 实验结果与限制

| 实验 | 观测结果 | 可支持的结论 |
|---|---|---|
| `v3-on-probe-20261008` | 60 条正常请求全部 HTTP 500；agent 日志显示实际上游 LLM 401 `InvalidApiKey` | 端到端真实模型评测被认证错误阻断 |
| `v2-control-auth-check-20261008` | 12 条正常请求全部失败，同类上游错误 | 控制版本也受相同认证问题影响，不能形成有效准确率对照 |
| `v3-jev-diagnostic-20261008.json` | 每组 80 条，正常误拦均 0；对抗阻断旧 19/20、新 18/20 | 初版存在对抗阻断退步，完整保留 |
| `v3-jev-diagnostic-refined-20261008.json` | 每组 175 条，正常误拦旧 10/155、新 0/155；对抗均 19/20；错误 0 | 此次真实 JEV 判定层观察到误拦减少 |
| `v3-jev-diagnostic-repeat-20261008.json` | 每组 175 条，正常误拦旧 9/155、新 0/155；对抗均 19/20；错误 0 | 重复一次仍观察到同方向误拦改善 |
| `v3-functional-20261008` | fake 30/30 | 脚本模型与隔离 fixture 的功能回归通过 |
| `v3-functional-expanded-20261008` | 首次扩展 fake 30/35，完整保留 | 主任务定位为新增场景期望问题；原始失败不能抹去 |
| `v3-functional-expanded-final-20261008` | 修正后 fake 35/35 | 最终功能回归通过 |

175 条每组由 55 条业务、100 条 OOS、20 条对抗组成。正常误拦分母为业务加 OOS 共 155 条，不能写成 175 条，也不能将判定层结果冒充端到端工具路由 Top-1、Reach 或真实客服完成率。详细数量和开始/结束时间见 `diagnostic-summary.json`；完整预测见三份诊断归档。诊断 JSON 的 `model` 为空，不能据此宣称 JEV 的确切模型或供应商版本。

这些诊断比较 legacy 与 candidate 问句配置，数据集是选取的项目样本，调用具有随机性；两次观测不是外部独立基准或大规模稳定性证明。不得声称 V3 准确率提升。对抗阻断也不等于越权或资金安全的端到端证明。HTTP 探针没有发送确认轮次，没有写业务；fake 功能回归使用隔离 fixture，功能测试的执行不能推导生产行为。

## 控制组溯源缺陷

原控制组 `run.json` 在当时没有设置 `SourceRevision`，其 `implementation.patch` 实际保存的是候选工作区 diff，**不能当作控制组真实源码**。原始 metadata 与 patch 逐字节保留，不做历史补写。

另附 `source-provenance.json` 记录主任务当时已知事实：控制 binary 在候选编译前复制，SHA256 为 `02d52fe76975ab562a63645024e7f5cdb64b157dd05bbc7957883258145ce181`，已知对应源码为 `b26642876e78255a89c87de3982888f14d6532d4`。原 `run.json` 也记录此 binary SHA。此补充记录基于本轮主任务观察，不能冒充原运行元数据或第三方可复现构建证明。原报告 `protocol`、`max_tokens`、`jev_model` 为 null，不能臆造这些配置。

## 行为评审文件压缩

四份 `eval/behavior-review/v1,v2/*.behavior.json` 的本地原文件保留，每份旁边新增 `.behavior.json.gz` 与 `.behavior.json.sha256.json`。原 JSON 合计 9,240,273 字节，压缩合计 636,457 字节，避免重复约 9 MB JSON 入 Git。它们的内容是派生行为评审，不是新真实模型运行；V1/V2 在该目录的含义以既有评审说明为准。

主 `manifest.json` 也记录这四份归档及其旁置 SHA manifest 的哈希。需要恢复时用 Python `gzip.decompress` 解压到另外的输出目录；不要覆盖历史原文件。

## 离线校验

在仓库根目录执行，只使用 Python 标准库，不调用模型或数据库：

```powershell
python eval/experiments/2026-10-08/verify.py
# 本地原文件仍在时，追加原字节对照：
python eval/experiments/2026-10-08/verify.py --check-sources
```

默认校验不依赖 `eval/reports` 中被忽略的原文件。校验 22 份归档的压缩哈希、解压哈希、原始大小、gzip 零时间戳、四份行为旁置 manifest 及两份补充记录哈希，错误返回非零退出码。成功输出末行：

```text
PASS 22 byte-exact archives; compressed bytes=702176; supporting provenance and summaries verified
```

哈希验证字节完整性，不是运行真实性的独立证明。未来实验写入新目录，不用新结果覆盖此次失败与初版退步。
