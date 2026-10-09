# 客服离线实验准备 Implementation Plan

> **For agentic workers:** 使用 superpowers:executing-plans、test-driven-development、dispatching-parallel-agents 和 verification-before-completion；独立文件范围并行，最后统一审查和验证。

**Goal:** 在零模型/JEV扣费的前提下，把下一阶段任务集、实验准备和运行入口整理为可校验、默认不运行的流程。

**Architecture:** 沿用既定客服评测契约与taskbench隔离租户。新增版本化开发边界小集；task-eval的prepare-only分支只读取本地案例、源码和二进制，不初始化数据库、不发送HTTP。实验脚本默认准备模式，真正运行必须显式开启并限制任务规模；V1=model、V2=jev_selective。

**Tech Stack:** Go、PowerShell、现有Python离线归档校验；不增加依赖。

**Spec:** [既定客服契约](../../customer-service-evaluation-contract-2026-10-09.md)。用户已授权继续后续开发、完成后推送，并明确本轮不得产生真实模型扣费；无需重复确认。

## Global Constraints

- 本轮不得请求真实模型或JEV，不跑在线评测，不读取或输出密钥。
- 1220条原文、既有归档和旧指标不变；开发小集不能称为独立盲测金标。
- 案例单独租户，必要追问、取消、确认、重复确认遵循共享规则。
- 只保存配置、SHA与任务数；不保存认证信息或推算未知价格的账单。
- 不修改无关plate_sim.py；完成后提交并推送已有origin/main。

## Review Focus

- prepare-only缺密钥/数据库/服务时仍能成功，不能触发HTTP或数据库连接。
- 拼错字段、未知工具、缺参分支缺补参、读写标志冲突必须在运行前拒绝。
- 默认脚本不能启动模型进程；超过上限即拒绝，不能先创建fixture后才检查。
- on/off与实际routing mode一致，不能遗留FastPath导致主实验混入历史模式。
- 所有输出独占创建；冻结manifest可在源码或案例改变时拒绝过期准备。

## Tasks

### 1. 开发边界样例与输入校验

Files: cmd/task-eval/cases.go, cases_test.go; eval/task-suites/2026-10-09-boundary-dev-v1/。

- [x] TDD验证未知字段、重复ID、非法工具、读写冲突和补参分支。
- [x] 增加有版本与SHA、合成fixture说明的开发小集；来源关联只使用实际1220 ID。
- [x] 小集验证与原有案例回归通过。

### 2. 完全离线prepare-only与配置冻结

Files: cmd/task-eval/main.go, config.go, prepare.go, prepare_test.go。

- [x] TDD验证无需写权限/密钥/HTTP/数据库、任务数上限、独占输出与哈希变化检测。
- [x] 分支在任何后端初始化前执行；准备manifest保存案例、规范、fixture代码、源码和二进制SHA。
- [x] 标明offline_preparation，不包含成绩、预测费用或在线模型能力证明。

### 3. 默认不扣费的实验脚本

Files: scripts/run-task-experiment.ps1; scripts/tests/test_run_task_experiment.py。

- [x] TDD用PowerShell真实脚本和本地替身可执行文件验证默认不启动、模式映射、任务上限、环境恢复。
- [x] 默认准备；显式RunEvaluation才运行，取消过时FastPath组合，传递cases/上限和运行mode。
- [x] 编写离线准备和以后小规模运行说明，不执行真实API。

### 4. 统一审查、验证与交付

- [x] 核对互相调用接口、报告限制、秘密泄漏与不可覆盖档案；审查发现运行配置错误仍可退出成功，已修复并加入回归。
- [x] Go全项目、Python全套、三项归档校验和diff检查。
- [x] 更新进度说明，准备可提交交付；最后执行提交、推送并核对远程HEAD。

## 验证结果

2026-10-10：go test ./...通过，Python61/61通过，go vet ./cmd/task-eval通过，旧档案校验通过；新二进制默认离线准备生成12任务清单，无真实模型/JEV调用、无任务fixture数据库写入。独立审查发现的配置错误退出码问题已修复，最终无遗留P1/P2。主实验的在线成绩、收费与独立盲测仍未运行。
