# 本地简体中文界面

这是固定 Langfuse 4.58.0 官方镜像的局部汉化层，不是官方完整国际化版本。覆盖登录、首页常用入口、设置菜单、导航、常用按钮、静态页面标题、输入输出面板和表格显示标题；部分说明、筛选字段名、外部链接仍保留英文。模型原始问答、评分名称/值、路由、列 ID、筛选参数、数据库内容均保留原文。

通过镜像已有 Meriyah 解析 JavaScript，只在确定的 UI 显示位置修改 AST 对应文本，不做全局替换或运行时调用翻译服务。动态表头只在 flexRender 渲染边界转换，保留原始列定义；条件式按钮仅翻译结果分支，设置菜单仅翻译具备 slug/content/cmdKKeywords 的标题；代码块、动态业务值、普通组件 label、无稳定 ID 的表头不改。输入输出面板 label 仅匹配已知显示属性组合。客户端和服务端同步处理，版本不符拒绝构建。原版 source map 仍指向上游源码，调试中文修改需对照 translate.cjs 和公开摘要清单。

## 构建与运行

在仓库根目录执行。私有 `.env` 沿用现有部署，不复制进构建上下文。

```powershell
docker build --network none --pull=false -t ai-gateway-langfuse-zh:4.58.0-local-v2 deploy/langfuse/zh
docker compose --env-file .worktrees/langfuse-observability/deploy/langfuse/.env -f deploy/langfuse/compose.yml -f deploy/langfuse/compose.zh.yml up -d --no-deps --pull never --wait --wait-timeout 120 langfuse-web
```

只更新 web，worker 和持久卷保持现状。后续管理中文部署时同时传两个 compose 文件；仅用原 compose 执行 up 会回到官方英文镜像。

访问 http://localhost:13000。旧页面普通刷新即可切换，不需要清除站点数据。v2 为中文资源生成基于内容摘要的 `/local-zh-<hash>/_next/static/` 地址，覆盖首屏、Next 页面预加载、Turbopack 懒加载与预渲染 HTML；修复了 v1 沿用英文 immutable URL 导致旧缓存继续显示英文的问题。Langfuse 页面没有传递 `__NEXT_DATA__.assetPrefix`，因此 Next 客户端的默认预加载前缀也必须同步设置。资源复制到 public 目录，由 Next 提供，不增加代理或翻译服务。未切换中文版的旧页仍需刷新一次。

## 验证与回退

构建执行 `node --test /opt/langfuse-zh/translate.test.cjs /opt/langfuse-zh/assets.test.cjs`；验证静态/动态标题、原始正文和代码保留、字段 ID/标签保留、重复执行稳定、无效脚本拒绝、资源隔离与不支持的加载器拒绝。构建生成 `/local-zh-manifest.json`，记录变更文件的前后摘要，不含密钥和评测正文。浏览器验证中文登录/设置菜单、追踪表头、agent.turn 问答标题、评分页，以及原 task_success/unknown 原文；检查普通刷新与页面预加载、懒加载全部使用中文资源前缀。v1 原始回执保留在 validation.json，v2 见 validation-v2.json。

回退只重建 web，不执行 down 或删除卷：

```powershell
docker compose --env-file .worktrees/langfuse-observability/deploy/langfuse/.env -f deploy/langfuse/compose.yml up -d --no-deps --pull never --wait --wait-timeout 120 langfuse-web
```

回退后也须 Ctrl+F5。正式升级 Langfuse 时应先更新并审查固定摘要、解析器路径、AST 匹配和浏览器测试；不能直接放宽版本校验。
