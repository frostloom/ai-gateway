# 本地简体中文界面

这是固定 Langfuse 4.58.0 官方镜像的局部汉化层，不是官方完整国际化版本。覆盖导航、常用按钮、静态页面标题、输入输出面板和表格显示标题；部分说明、筛选字段名、外部链接仍保留英文。模型原始问答、评分名称/值、路由、列 ID、筛选参数、数据库内容均保留原文。

通过镜像已有 Meriyah 解析 JavaScript，只在确定的 UI 显示位置修改 AST 对应文本，不做全局替换或运行时调用翻译服务。动态表头只在 flexRender 渲染边界转换，保留原始列定义；代码块、动态业务值、普通组件 label、无稳定 ID 的表头不改。输入输出面板 label 仅匹配已知显示属性组合。客户端和服务端同步处理，版本不符拒绝构建。原版 source map 仍指向上游源码，调试中文修改需对照 translate.cjs 和公开摘要清单。

## 构建与运行

在仓库根目录执行。私有 `.env` 沿用现有部署，不复制进构建上下文。

```powershell
docker build --network none --pull=false -t ai-gateway-langfuse-zh:4.58.0-local-v1 deploy/langfuse/zh
docker compose --env-file .worktrees/langfuse-observability/deploy/langfuse/.env -f deploy/langfuse/compose.yml -f deploy/langfuse/compose.zh.yml up -d --no-deps --pull never --wait --wait-timeout 120 langfuse-web
```

只更新 web，worker 和持久卷保持现状。后续管理中文部署时同时传两个 compose 文件；仅用原 compose 执行 up 会回到官方英文镜像。

访问 http://localhost:13000。**已打开英文版的浏览器首次必须 Ctrl+F5 强制刷新；若仍有旧资源，清除此站点的缓存后重新登录。** 官方静态 chunk URL 保留，仍有 immutable 缓存；本地部署采用首次清缓存作为切换步骤，不能把普通刷新视为可靠验收。不得在同一地址并行混用英文和中文 web 实例。独立浏览器验证先加载旧页，再清 HTTP 缓存检查汉化。

## 验证与回退

构建执行 `node --test /opt/langfuse-zh/translate.test.cjs`；验证静态/动态标题、原始正文和代码保留、字段 ID/标签保留、重复执行稳定及无效脚本拒绝。构建生成 `/local-zh-manifest.json`，记录变更文件的前后摘要，不含密钥和评测正文。浏览器验证登录、追踪表头、agent.turn 问答标题、评分页，以及原 task_success/unknown 原文。

回退只重建 web，不执行 down 或删除卷：

```powershell
docker compose --env-file .worktrees/langfuse-observability/deploy/langfuse/.env -f deploy/langfuse/compose.yml up -d --no-deps --pull never --wait --wait-timeout 120 langfuse-web
```

回退后也须 Ctrl+F5。正式升级 Langfuse 时应先更新并审查固定摘要、解析器路径、AST 匹配和浏览器测试；不能直接放宽版本校验。
