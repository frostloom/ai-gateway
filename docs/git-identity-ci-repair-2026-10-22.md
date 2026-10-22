# Git 身份与 CI 修复（2026-10-22）

GitHub SSH 认证账户是 frostloom。此前仓库没有配置提交身份，Git 自动使用本机用户名与主机邮箱，导致推送后的作者归属不正确。仓库本地 author/committer 配置现统一为 `frostloom <278895495+frostloom@users.noreply.github.com>`；GitHub 用户 ID 为 278895495。

用户要求统一所有历史提交身份。操作范围覆盖远程 main 和三个历史分支；先保存完整 bundle，再在隔离 bare 仓库内重写作者与提交者。逐提交核对 tree、消息、日期与映射后的父提交，保持代码及合并关系不变；固定各远程旧 SHA 后以 force-with-lease 发布。原始 SHA 仍保留在历史评测证据中，不能冒充重写后重新执行的证据。恢复 bundle、提交映射与核验结果保留在本机 `.tmp/git-author-rewrite-20261022/`。

CI 的实际失败来自 `scripts/build-paired-live.py` 将临时目录写死为 macOS 的 `/private/tmp`；Ubuntu 没有该目录，`TestArchiveBuildScriptBuildsCommittedSourceAndRecordsHashes` 在归档构建前抛出 FileNotFoundError。改用 Python 系统临时目录选择，支持 TMPDIR。

回归测试覆盖实际 Git 归档、系统临时目录选择、编译失败后的目录清理，以及不产生二进制与 provenance 文件。已观察原实现失败、修复后通过；Linux 与 macOS 的 paired-live Go 包通过，Python 全套 171 项（15 项跳过）通过。官方 Go 1.25.12 Linux 容器中 `go vet ./...` 与 `go test ./... -count=1` 全部通过（AGENT_LIVE_JEV=0，连接本机测试 MySQL/Redis）。远程 CI 需在推送后另行确认。
