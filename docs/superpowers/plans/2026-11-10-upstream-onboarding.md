# 上游路径、模型发现与批量建模

用户要求修复线上 GLM 的 404，同一个用户 API Key 能调用所有已上架模型，并提供上游连通检测、支持模型发现、批量创建定价与上下文自动带入。

已验证 GLM 配置为 https://open.bigmodel.cn/api/coding/paas/v4，GET models 返回真实列表。旧 Chat Completions 追加 /v1 产生 /v4/v1/chat/completions，返回 404。旧 Messages 丢失配置路径而发送到站点 /anthropic/v1/messages，实际 GLM 使用 /api/anthropic/v1/messages。

1. 统一上游 URL 解析，保留自定义路径，避免重复版本段。为 Messages 与 Responses 提供可选协议 Base URL，现有渠道使用兼容默认值，GLM Coding 地址自动映射原生协议路径。
2. 管理员检测已保存或草稿上游，鉴权 GET models，解析真实模型与上下文等元数据。服务端设置超时、长度限制、禁止带密钥跨域重定向，输出脱敏错误。
3. 原子批量导入经发现或已明确配置的模型，合并渠道支持列表，保留已有模型价格与状态，新增模型需明确价格和上下文。未知元数据统一补填。
4. 弹窗内提供检测、选择、批量填写与创建，展示真实来源及已存在状态，处理失败、重复提交、配置修改后过期结果和键盘操作。
5. 使用同一个测试 Key 覆盖多个模型和请求协议，定向回归、浏览器验收及 code-review 后只发布网关相关服务，核对 Myink。

使用 systematic-debugging、frontend-design、Taste、Hallmark 和 subagent-driven-development。用户已授权直接完成及发布。现有 dirty 工作保留，旧评测 scope 保持永久 closed。

## 验证与审核

- URL 路径、gateway/handler、router、billing/admin、billing/store 定向回归通过。
- 独立临时 MySQL 库验证事务回滚、已有模型保留、过期配置与禁用状态，运行后已删除该测试库。
- 同一个用户 Key 通过实际鉴权中间件和 gRPC/HTTP 客户端调用两个模型、Chat/流式/Messages/Responses；上游密钥隔离与结算通过。
- Web verify、组件 jsdom 与桌面/390px 手机弹窗检查通过。
- code-review Standards/Spec 各发现相同两项 P2：选择超过100项、发现元数据超出导入字段约束。已修复选择限制、编号禁选原因和名称/厂商编辑校验，独立复审无新增P1/P2。
- 选择已有模型不覆盖原价格与状态；上游未提供上下文时要求明确补填。
