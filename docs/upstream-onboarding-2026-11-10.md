# 上游接入与 GLM 修复

用户 API Key 绑定账户，不绑定单个模型。使用同一个 Key，通过请求中的 `model` 选择管理员已配置且已上架的模型；账户状态、余额、月卡额度与限流仍照常校验。

线上 GLM 的 OpenAI 兼容 Base URL 是 `https://open.bigmodel.cn/api/coding/paas/v4`。Chat Completions 路径应为其后追加 `/chat/completions`，旧实现多追加 `/v1` 导致 404。Messages 路径为 `/api/anthropic/v1/messages`，旧实现丢失 `/api`。现已统一解析路径，并支持渠道的可选 Messages、Responses Base URL。Messages 使用渠道密钥设置 Bearer 与 `x-api-key`，不会把用户 Key 转交上游。推理和发现请求均禁止自动跟随重定向。

管理员在「上游渠道」卡片点击「检测并发现模型」，或在编辑弹窗中检测草稿。检测会鉴权读取真正的上游模型列表，不发生成请求。保存后的渠道可选择模型、统一填写输入和输出单价并导入。每次最多100项，新增定价与渠道路由在同一事务提交；已有模型的价格、上下文与上下架状态保留。

上下文优先采用上游元数据；未提供则采用相同模型编号的已有目录值，并显示来源。两者均没有时，需要逐项补充或统一填入。不会猜测模型上下文或价格。发现接口不能保证账户对列表中每个模型都具备生成额度；实际推理权限仍由上游账户控制。

网关管理员接口：

- `POST /admin/channels/discover`：已保存渠道传 `provider_id`，草稿可传 `base_url`、`upstream_key`；不传新密钥时可使用已保存密钥。
- `POST /admin/models/import`：传 `provider_id` 与 `items`，新增项需明确上下文及输入/输出价格。已存在项只传 `model_id`。

用户接口：OpenAI SDK 使用网关 `/v1` Base URL；Messages 客户端使用网关 `/anthropic` Base URL并请求 `/v1/messages`，也支持网关 `/v1/messages`。同一个用户 Key 可用于上述接口。上游须支持请求所选协议，网关不会伪造供应商未提供的协议能力。

## 验证

- URL解析、网关处理器、路由、管理员与计费存储定向测试通过；相关包竞态测试通过。
- 独立临时 MySQL 测试库验证导入回滚、旧定价和状态保留、过期配置拒绝、禁用渠道拒绝，测试库已删除。
- 正常鉴权中间件结合 loopback gRPC/HTTP 验证同一个 Key 调用两个模型及 Chat、流式、Messages、Responses，校验独立上游密钥、结算与错误 Key 拒绝。
- 重定向回归覆盖 Chat/Messages、流式/非流式及302/307/308，目标站点未收到任何请求，预扣被撤销。
- Web verify、组件 jsdom、桌面与390px手机弹窗验收通过。
- code-review Standards/Spec 发现选择上限及元数据约束两项问题，已修复并复审通过。

发布仅替换 gateway、billing、router 二进制，沿用现有 agent 等进程二进制；已有兑换码、套餐和财务流程保持。旧评测执行范围不重启、不续领。

## 线上结果

已发布镜像 `aigateway:upstream-20261110-r1`。Origin 携带端口的管理员登录、全部进程健康与鉴权检查通过。GLM 真实目录返回12个模型，原有SKU价格与配置未改变。未自动上架未定价的新模型。

使用独立验收账户，经管理员生成并兑换1元券后，创建一个临时用户Key，仅进行了以下4次真实推理，每次最大输出16token，无重试：

| 模型 | 接口 | 结果 | 时长 | 输入/输出token |
|---|---|---|---|---|
| deepseek-flash | Chat Completions | 200 | 0.752s | 8/1 |
| glm-5.3 | Chat Completions | 200 | 3.109s | 16/16 |
| glm-5.3-flash | Chat Completions | 200 | 1.198s | 16/9 |
| glm-5.3 | Messages | 200 | 1.535s | 16/3 |

4笔reserve与4笔settle均为settled，无待处理账单；临时用户、租户和Key均已停用，验收账本保留。1元为券入账额，不是上游实际花费。

发布后服务器可用内存约1978MiB，网关进程容器约35MiB/360MiB，Kafka约375MiB/768MiB。Myink所有容器ID、启动时间和健康状态与发布前一致。配置与数据库备份及回滚镜像保留于服务器本次release目录。
