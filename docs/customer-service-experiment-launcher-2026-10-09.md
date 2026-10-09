# 客服实验：离线准备入口

`scripts/run-task-experiment.ps1` 默认只调用本地 task-eval 的 `-prepare-only`。它不载入 `.env`、不启动 agent、不检查数据库，也不调用模型或 JEV。必须显式传入模型名称，这个名称只是准备配置。

```powershell
.\scripts\run-task-experiment.ps1 `
  -JevMode off -Version V1 -Model '<计划使用的模型名称>' `
  -AgentExe 'bin/agent.exe' -EvaluatorExe 'bin/task-eval-v2.exe' `
  -SourceRevision HEAD -Out 'eval/runs/2026-10-09-v1-preparation'
```

运行前先构建本地 agent 与 task-eval，并使用尚不存在的输出目录。默认案例文件是 `eval/task-suites/2026-10-09-boundary-dev-v1/cases.json`，默认上限为 12；案例总数超过 `-MaxTasks` 时拒绝整个运行，不会截断任务集。任务数上限不是 API 调用数或金额预算：追问、确认等多轮流程可以产生多次模型调用。开发边界小集用于开发验证，不是独立盲测金标。

`-JevMode off` 映射为 `model`；`on` 映射为 `jev_selective`。可显式给出相同的 `-RoutingMode`，冲突会被拒绝。旧 `-FastPath` 参数已取消。`SourceRevision` 会解析为完整 Git commit，manifest 冻结案例、代码和二进制的校验值。`prepare-exit-code.txt` 保存准备进程的退出码；成功的 `prepare.json` 只证明本地准备通过，不代表模型成绩或费用。

以后获得在线运行授权时，在同样参数上加 `-RunEvaluation -Port 8087`，并使用另一个全新输出目录。该开关会先完成本地准备，再载入 `.env`、设置端口/模型/路由模式、启动 agent，并把 `prepare.json` 通过 `-prepared` 传入评测器重新校验。在线运行可以产生真实模型/JEV费用，需要真实数据库配置，并且只允许隔离的模拟租户写入。脚本退出时会终止它启动的 agent，恢复全部进程环境变量；`run.json`、agent 日志、`tasks.json` 和 `exit-code.txt` 留在输出目录。退出码 1 表示任务评测不通过，超过 1 表示基础设施失败。

2026-10-09 的准备工作不执行该在线开关。脚本回归测试使用本地替身程序与本机 health 服务，不请求付费 API。
