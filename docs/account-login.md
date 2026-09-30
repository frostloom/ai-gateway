# 账号注册与登录

- 用户入口：`http://localhost:18080/portal`，选择「普通用户」，点击「直接注册」。用户名为 3–32 位字母、数字、下划线或短横线，统一转为小写；密码为 8–72 字节。
- 管理入口：`http://localhost:18080/`，选择「管理员」。已有管理员账号及密码保留；没有管理员时显示首次初始化。公开注册不能创建管理员。
- 新用户自动分配独立租户，初始余额为零。刷新页面恢复会话，退出登录撤销服务端凭证。客服、订单、订阅、消费数据均绑定当前租户。
- 用户和管理员分别使用 `agw_user`、`agw_admin` HttpOnly cookie，有效期 12 小时。用户会话凭证只保存 SHA-256 哈希，密码使用 bcrypt；网关直接使用 HTTPS 时设置 Secure。HTTPS 在反向代理终止时，给 gateway 设置 `AUTH_COOKIE_SECURE=1`，不信任客户端自行提供的协议头。本机 HTTP 测试不要开启此选项。
- 浏览器调试请求改走 `/portal/completions`，使用登录会话；外部 `/v1/chat/completions` 保留原 API Key 认证。原有脚本携带租户 API Key 访问 `/portal/*` 仍兼容；浏览器有用户 cookie 时优先校验会话，拒绝回退到其他租户的 Key。
- 登录/注册按真实对端 IP 限制为 5 分钟 30 次。跨站浏览器写请求被拒绝。`ADMIN_TOKEN` 只作为显式配置的管理脚本凭证，不再提供代码内置默认值。

接口：

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | `/auth/register` | `{username,password}` 注册普通用户并登录 |
| POST | `/auth/login` | `{username,password}` 用户登录 |
| GET | `/auth/me` | 当前用户身份与所属租户，未登录返回 401 |
| POST | `/auth/logout` | 撤销当前会话 |
| POST | `/admin/auth/login` | 管理员登录 |
| POST | `/admin/auth/setup` | 仅首次初始化管理员 |

启动：`./scripts/start-all.ps1 -Rebuild`。已有开发数据库由 billing 的 AutoMigrate 补建 `portal_users`、`portal_sessions`，保留已有账号和业务数据；新库完整结构在 `scripts/schema.sql`。

回归测试覆盖注册事务、重复用户名、密码错误、角色越权、退出与过期、禁用账号、跨站请求、旧 API Key 与网页登录身份冲突，以及客服会话的跨租户拒绝。
