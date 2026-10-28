# 窗口正文投影与Langfuse校验

评分适配器现在能在独立副本调用冻结源码的原`Observer.content()`，以内存exporter生成预期正文，不读取凭据或外发trace。完整源码清单、helper、可信Go二进制、原记录及输出过程摘要前后校验；普通JSON或手写摘要不能替代本次进程的proof。原脱敏、UTF8裁剪、4096字节包装与空回复规则均实际自测通过。

实际验证中先发现合法归档root `.git` pointer被预检误拒；修正仅放行该受限普通文件，不复制进副本、不忽略其他额外文件。第二次helper通过后首GET发现Langfuse将字符串false保存为布尔false；兼容仅限两个已知布尔metadata字段的同值表示，整数0/1、错值和非规范字符串继续拒绝。两次失败证据保留，不重写为首次成功。

最后一次对新v3首窗56条实际trace、235项observation、60轮完成只读核验：scope/window/pass/ordinal/arm/order/release/environment、根状态、父链、完整轮次和原脱敏正文均匹配。原生LLM请求没有IO的观测不被冒称已捕获，携带资料不证明模型实际使用。缺失/延迟读取保持未验证；其他窗口尚未完成正式内容读回。

专项53项通过；完整Python回归253项、15项原条件跳过、0失败，耗时30.702秒。三次独立源码/定向复核无确认P1/P2。Go运行只选择固定helper，不修改冻结归档或执行器，不额外重跑整套业务测试。

仍为`publish_ready=false`、0评分发布。prior receipt链和单次发布协议由后续Task3完成，不能用首窗验证代替全量评分、全量正文读回或原3660对目标。[机器证据](evidence/full1220-window-content-2026-10-29.json)提供源码和验证摘要SHA。
