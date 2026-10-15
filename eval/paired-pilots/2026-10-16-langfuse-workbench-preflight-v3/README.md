# 工作台接入后的运行准备 v3

离线调用原有 prepare-observed-customer-pilot.py 的 prepare(root) 创建，未更改冻结 v2。manifest schema 仍为已支持的 observed-preparation-manifest.v2；目录 v3 表示新的证据版本，不代表更改合同格式。

保留 1220 主任务、2440 实验臂和单列的 1851 边界。绑定新增基础提示词观测关联后的运行代码。

startup-check.json：直接使用默认 .env 的检查失败；渠道、模型、协议、JEV 版本不符，未尝试远程推理。

startup-check-selected-profile.json：最后加载现有 deploy/langfuse/evaluation.env.example，并仅把本机 Langfuse endpoint 改为 127.0.0.1 后通过。只验证字段、代码绑定及本机 Langfuse 健康/认证，不能证明外部密钥有效。

两个检查均为零模型/JEV 调用，ready_for_paid_run=false。下一步仍需独立审核、真实小样本验收与费用授权。
