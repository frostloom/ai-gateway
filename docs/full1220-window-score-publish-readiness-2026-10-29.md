# 原完整窗口评分发布协议验收

Task3现在提供默认只读的plan/verify，以及显式publish --execute。发布前重算原summary→receipt与prior-summary完整SHA链，核对累计observed/quality/unrun分项；已真实验证至停止窗9，部分执行及未运行分母保持原值。固定标准库Go helper只读验证原Ed25519 attestation及二进制SHA，真实签名通过、改一位签名拒绝，没有执行已消耗的评测二进制或读取私钥。

全部trace按冻结content()的opaque proof实际读回通过后，固定本机项目registry先O_EXCL/fsync保存原完整batch目录，再按稳定scope/window/pass/ordinal/arm/trace/metric保存logical intent、score ID及sent证据，随后每分POST一次并保存accepted。更换review、ID、输出目录或脚本副本不改变该锁；POST/ACK/落盘失败保留未知状态，只能读回，不自动重发。registry绑定本项目固定路径与目录身份，不承诺抵抗同权限恶意删除所有磁盘证据。

独立审查确认P2：原verify仅验证现存intent，丢失1组证据仍将11/12误报完整。保留RED后以发布前完整batch清单修复；独立原复现现在返回unverified，原ID不能重发。新review少指标时仍核对原始完整payload。wire中移除仅用于本地锁的logical_identity，HTTP仅携带标准score字段。

72项专项及完整Python272项通过，15项原条件跳过、0失败，完整用时41.409秒；3项独立定向复核通过，无剩余确认P1/P2。评测执行器归档和客服版本保持原绑定。

本报告为实际评分发布前快照：482条已采集trace正文已完整核验，评分POST仍0。接下来仅发布该停止范围的独立评分并只读验证；原完整3660对未完成，不能用当前交付代替最终全量报告。[机器证据](evidence/full1220-window-score-publish-readiness-2026-10-29.json)记录源码、测试和签名验证SHA。
