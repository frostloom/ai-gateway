# 控制台重构交付与发布核验

## 交付范围

用户控制台采用 OpenCode Console 公开主题与活动页样式，顶部导航、真实消费图表、余额与月卡进度统一。管理员上游与套餐采用 aixcoder 资源卡片。客服常驻负责账户服务和操作指引，模型对话单独位于工作台。模型目录与启用上游支持集合相交。

管理员可生成一次性余额码，用户通过鉴权账户兑换，明文只在签发响应出现。充值建单、模拟支付、月卡开通与换套餐在 gateway、billing admin 和客服工具均无条件关闭。取消已有续费保留。

新月卡显示滚动小时、每周和周期的金额额度、已使用百分比、剩余金额与恢复时间。旧订阅保留权益，仅显示档次百分比与滚动小时。保存条款固定，可复制新套餐并启停。

## Standards

code-review 固定点为 bad0ceeced451820304337fb22ea8b4882cc2a0c，包含 WIP 与新增文件。原始工作区快照存于 .tmp/frontend-redesign-20261109/before.patch。

发现并修复两项 P1。

- 金额预占先普通读取订阅会在 REPEATABLE READ 下建立旧快照。首次查询改为 FOR UPDATE，获取锁后才读取额度。
- 批量账本的兑换不得用较高旧 SQL 覆盖 Redis 扣款。存在 pending 或余额差额时直接拒绝，不按旧券金额差额自动恢复。

定向复核无未闭发现。正常、race 回归覆盖旧快照、fresh batch 差额、旧券重放和单次入账。

## Spec

发现并修复充值演示开关绕通、历史月卡余量不可见、客服不理解金额权益，以及月卡开通和换套餐的演示绕通。目录与账户展示均对齐最新要求。已完成只读自复核，未保留环境变量绕通。

另一次安全复审确认旧券差额恢复具有不可区分的资金状态，已删除该例外，最终源码复审无其他可复现新增 P1/P2。

## 验证

- npm run verify 通过，包含类型检查、生产构建、登录/客服确认、审计、工作台多轮模型传递、充值关闭、券交互、三周期金额百分比、管理员签发和 Modal 焦点。
- store 全套通过，57.280 秒。
- billing admin 全套通过，1.263 秒。
- agent 全套通过，33.051 秒。
- gateway handler 全套通过，1.299 秒。
- router 全套通过，1.009 秒。
- 金融定向 normal 与 race 均通过，最终 10 项分别 5.213 / 7.173 秒。admin 与 agent 最终门禁定向 race 通过。
- 本机默认 CGO SDK 链接不兼容，normal 使用 CGO_ENABLED=0，race 使用 MacOSX15.4 SDK，有版本链接警告。Linux amd64 四个发布二进制均使用 CGO_ENABLED=0 构建。
- Hallmark 六个文件、两个渲染页面为 0 FAIL、0 WARN。19 项聊天控件间距保留既有 2px 增量并记录 waiver。
- 浏览器检查桌面和 390px 手机布局，上游、工作台、概览、月卡及客服。概览、上游、工作台和月卡最终 axe 检查均无违规。月卡标签和后台灰底文字对比度已修复。

OpenCode 私人工作区未读取，参考来自目标地址公开加载的主题、activity 与 shell 样式。本轮验证未发起真实模型、JEV 或历史评测请求。

## 发布

最终镜像为 aigateway:console-20261110-r2，运行于 /home/ubuntu/aigateway/docker-compose.yml 的 app 服务。独立 aigw-edge 启用 HTTP 308 到 HTTPS 跳转并拒绝公网 /metrics。只替换网关 app，独立 edge 采用 nginx -t 后 reload。

服务器保留 releases/console-20261110 的原 compose、edge 配置、网关数据库备份和 rollback-console-20261110 镜像。第一次发布的验收脚本将管理入口误写为 /dashboard.html，收到 404 后自动回滚。更正为实际 / 后完整 27 项验收通过。随后自定义周期标签修正的发布准备首次只读生成失败，已改为从不可变原配置生成，未更改运行服务。最终 r2 重新通过 12 项页面、鉴权、管理员接口与健康检查，后端二进制与完整验收版本一致。

完整验收覆盖用户/管理员登录与隔离、Secure Cookie、no-store、模型/套餐/用量/API 接入、四个资金入口 403 和无效兑换码 400。创建了一个零余额专用验证账户，退出会话后已停用其用户与租户。没有实际资金变动或模型调用。

发布前后六个 Myink 容器的完整 ID、StartedAt、运行与健康状态逐项相同，未变更其服务或证书卷。发布脚本两次切换前均暂停网关新请求并等待 135 秒落盘。最终二进制 SHA256 与本地发布 manifest 一致。

用户入口为 https://myink.cloud:18080/portal，管理员入口为 https://myink.cloud:18080/。新月卡开通与换套餐继续关闭，待安全支付流程接入。私人 OpenCode 工作区内容未读取，效果依据公开样式及本地替身账户核验。

详细验证原件位于 .tmp/frontend-redesign-20261109/，服务器回执位于 /home/ubuntu/aigateway/releases/console-20261110/。

## 浏览器登录同源校验修复

用户反馈 cross-origin request rejected。复现证实 nginx 的 proxy_set_header Host $host 丢失了公开入口的 18080 端口，gateway 因 Origin authority 与 Request.Host 不一致拒绝真实浏览器登录。先前 urllib 验收缺少 Origin，未覆盖此问题。

2026-11-10 将独立 aigw-edge 配置改为 proxy_set_header Host $http_host，保留完整公开 authority。nginx -t 成功后 reload，发布配置生成器同步修复。验收脚本所有请求补上真实 Origin 和 Sec-Fetch-Site。

修复后正确 origin https://myink.cloud:18080 登录返回 200，无端口 origin 和第三方 origin 均返回 403，Secure Cookie 保留。真实 Chromium 浏览器通过登录表单进入管理员总览，带 Origin 的 12 项线上检查通过。六个 Myink 容器 ID、启动时间与健康状态再次核对一致。
