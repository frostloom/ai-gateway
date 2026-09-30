-- AI 模型网关 / 计费平台 建表脚本（MySQL 8.0, InnoDB, utf8mb4）。
-- docker-compose 启动时由 mysql 容器自动执行（/docker-entrypoint-initdb.d/）。
-- 注意：bills.uk_request_phase 唯一索引是「幂等」的根——重复结算/冲正靠它收敛成 no-op。

CREATE DATABASE IF NOT EXISTS ai_gateway DEFAULT CHARACTER SET utf8mb4;
USE ai_gateway;

CREATE TABLE IF NOT EXISTS tenants (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name          VARCHAR(64) NOT NULL,
  initial_quota BIGINT NOT NULL DEFAULT 0 COMMENT '初始额度；对账重建 balance 的依据',
  status        TINYINT NOT NULL DEFAULT 0 COMMENT '0=active 1=disabled',
  created_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS api_keys (
  id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id  BIGINT UNSIGNED NOT NULL,
  key_hash   CHAR(64) NOT NULL COMMENT 'SHA-256(key) 全文；明文 sk- 永不落库',
  key_prefix VARCHAR(16) NOT NULL COMMENT '明文前 8 位，日志/排查用',
  name       VARCHAR(64) NOT NULL DEFAULT '',
  status     TINYINT NOT NULL DEFAULT 0 COMMENT '0=active 1=disabled',
  expires_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_key_hash (key_hash),
  KEY idx_tenant_status (tenant_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS providers (
  id               BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name             VARCHAR(64) NOT NULL,
  base_url         VARCHAR(255) NOT NULL,
  upstream_key     VARCHAR(255) NOT NULL DEFAULT '' COMMENT '上游 API key（demo 明文存；生产接 KMS/密文）',
  cost_in_cent     BIGINT NOT NULL DEFAULT 0 COMMENT '上游成本：输入 分/百万 token（毛利=售价−成本）',
  cost_out_cent    BIGINT NOT NULL DEFAULT 0 COMMENT '上游成本：输出 分/百万 token',
  models           JSON NOT NULL,
  weight           INT NOT NULL DEFAULT 1 COMMENT '路由权重',
  status           TINYINT NOT NULL DEFAULT 0 COMMENT '0=active 1=disabled 2=banned',
  fail_count       INT NOT NULL DEFAULT 0,
  consecutive_fail INT NOT NULL DEFAULT 0,
  cooldown_until   DATETIME(3) NULL,
  created_at       DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at       DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_name (name),
  KEY idx_status_weight (status, weight)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS bills (
  id                 BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  request_id         VARCHAR(64) NOT NULL,
  phase              VARCHAR(16) NOT NULL COMMENT 'reserve|settle（一条请求两行账目）',
  status             VARCHAR(16) NOT NULL COMMENT 'reserve行: pending|settled|reversed；settle行: settled',
  tenant_id          BIGINT UNSIGNED NOT NULL,
  api_key_id         BIGINT UNSIGNED NOT NULL,
  provider_id        BIGINT UNSIGNED NULL,
  model              VARCHAR(64) NOT NULL DEFAULT '',
  funding            VARCHAR(16) NOT NULL DEFAULT 'balance' COMMENT 'balance 余额扣费 | subscription 订阅额度（token 不扣钱，M9）',
  in_price_cent      BIGINT NULL COMMENT 'reserve 时目录价快照：输入价 分/百万token（改价不影响历史账单）',
  out_price_cent     BIGINT NULL COMMENT 'reserve 时目录价快照：输出价 分/百万token',
  pre_quota          BIGINT NOT NULL COMMENT 'reserve 预扣（分）',
  actual_quota       BIGINT NULL COMMENT 'settle 实际（分）',
  delta_quota        BIGINT NULL COMMENT 'actual - pre（settle 行，分）',
  prompt_tokens      INT NULL,
  completion_tokens  INT NULL,
  usage_reported_at  DATETIME(3) NULL COMMENT '网关流终态上报用量的持久 marker',
  error_code         VARCHAR(64) NULL,
  created_at         DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_request_phase (request_id, phase),              -- 幂等：结算/退款唯一
  KEY idx_tenant_status_created (tenant_id, status, created_at), -- 租户账单查询
  KEY idx_recon (status, usage_reported_at, created_at),         -- 对账扫描
  KEY idx_provider_created (provider_id, created_at)             -- provider 成本分析
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ---------- M7 自服务：套餐 / 订阅 / 充值 / 入账 / AI 客服 ----------

-- ---------- 商品目录（计费权威：reserve/settle 按目录单价计价，admin 可改价/启停） ----------

CREATE TABLE IF NOT EXISTS models (
  id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  vendor            VARCHAR(32) NOT NULL COMMENT '厂商：DeepSeek/通义千问/智谱/Kimi/...',
  model_id          VARCHAR(64) NOT NULL COMMENT '调用名 = 计费键，全站唯一',
  name              VARCHAR(128) NOT NULL COMMENT '展示名',
  input_price_cent  BIGINT NOT NULL DEFAULT 0 COMMENT '输入价 分/百万token',
  output_price_cent BIGINT NOT NULL DEFAULT 0 COMMENT '输出价 分/百万token',
  context_len       INT NOT NULL DEFAULT 0 COMMENT '上下文窗口（K token）',
  tags              JSON NULL COMMENT '["文本","推理","旗舰"]',
  status            TINYINT NOT NULL DEFAULT 0 COMMENT '0=上架 1=下架',
  created_at        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_model_id (model_id),
  KEY idx_vendor_status (vendor, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS plans (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name          VARCHAR(64) NOT NULL,
  plan_type     VARCHAR(16) NOT NULL COMMENT 'recurring 定期订阅（GPT Plus 式：月费 + 每窗口 token 额度）',
  price_money   BIGINT NOT NULL COMMENT '月费（元，模拟价，不接真钱）',
  validity_days INT NOT NULL DEFAULT 0 COMMENT '计费周期天数（续费/改套餐周期）',
  refresh_hours INT NOT NULL DEFAULT 5 COMMENT '额度滚动窗口小时数（M9：每 N 小时刷新，未用不累积）',
  tier_quota    JSON NOT NULL COMMENT '每窗口每档次的 token 额度 {"轻量":2000000,"文本":1000000,...}',
  status        TINYINT NOT NULL DEFAULT 0 COMMENT '0=active 1=disabled',
  created_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS subscriptions (
  id             BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id      BIGINT UNSIGNED NOT NULL,
  plan_id        BIGINT UNSIGNED NOT NULL,
  plan_type      VARCHAR(16) NOT NULL,
  status         VARCHAR(16) NOT NULL COMMENT 'active|cancelled|expired',
  cycle_start    DATETIME(3) NULL COMMENT 'recurring 本期起止；one_time 为 NULL',
  cycle_end      DATETIME(3) NULL,
  auto_renew     TINYINT NOT NULL DEFAULT 0 COMMENT 'recurring 退订 = 置 0（当期不退）',
  cycle_num      INT NOT NULL DEFAULT 1 COMMENT '已续期数（topup ref 用）',
  quota_granted  BIGINT NOT NULL DEFAULT 0 COMMENT '累计入账额度（分）',
  pending_plan_id BIGINT UNSIGNED NULL COMMENT '改套餐：下期生效',
  cancelled_at   DATETIME(3) NULL,
  created_at     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  KEY idx_tenant_status (tenant_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS recharge_orders (
  id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id    BIGINT UNSIGNED NOT NULL,
  order_no     VARCHAR(64) NOT NULL COMMENT '幂等锚点（类比 bills.request_id）',
  amount_money BIGINT NOT NULL COMMENT '模拟元（wire 层整数收）',
  amount_cent  BIGINT NOT NULL COMMENT '到账金额（分）= amount_money * 100',
  status       VARCHAR(16) NOT NULL COMMENT 'pending_payment|paid|refunded|cancelled',
  paid_at      DATETIME(3) NULL,
  created_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_order_no (order_no),
  KEY idx_tenant_status (tenant_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS topups (
  id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id  BIGINT UNSIGNED NOT NULL,
  source     VARCHAR(16) NOT NULL COMMENT 'recharge|subscription|refund',
  amount     BIGINT NOT NULL COMMENT '正=入账，负=退款',
  ref_type   VARCHAR(32) NOT NULL,
  ref_id     VARCHAR(64) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_ref (ref_type, ref_id),  -- 幂等：重试/并发只入账一次（正余额账本的提交点）
  KEY idx_tenant_created (tenant_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS agent_sessions (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  session_id    VARCHAR(64) NOT NULL,
  tenant_id     BIGINT UNSIGNED NOT NULL,
  api_key_id    BIGINT UNSIGNED NOT NULL,
  created_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  last_active_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_session (session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS agent_audit_log (
  id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  session_id VARCHAR(64) NOT NULL,
  tenant_id  BIGINT UNSIGNED NOT NULL,
  tool       VARCHAR(32) NOT NULL,
  args       TEXT NULL COMMENT '参数 JSON',
  confirmed  TINYINT NOT NULL DEFAULT 0 COMMENT '写操作是否经用户确认',
  preview    VARCHAR(255) NOT NULL DEFAULT '' COMMENT '确认时展示的预览文案',
  result     TEXT NULL COMMENT '执行结果/错误',
  guard      TEXT NULL COMMENT 'P3 Jev 安全决策 JSON（拦截原因等，可空）',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_session (session_id),
  KEY idx_tenant_created (tenant_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ---------- M10 Kafka 计费事件流：事务性 outbox + 审计明细 ----------

-- outbox：bill 变更与 outbox 行同一 MySQL 事务提交（记账事件不丢不重，relay 轮询投递）。
-- 幂等锚 = uk_event_key（{request_id}:{phase}）：同一事件只入一行，重试撞唯一索引收敛 no-op。
CREATE TABLE IF NOT EXISTS outbox_events (
  id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  topic      VARCHAR(64) NOT NULL COMMENT 'Kafka topic（billing.events / billing.events.dlq）',
  event_key  VARCHAR(128) NOT NULL COMMENT '{request_id}:{phase} 幂等键',
  payload    JSON NOT NULL COMMENT '事件体（审计所需字段全部内嵌，自包含）',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  sent_at    DATETIME(3) NULL COMMENT 'relay 已投递时间（NULL=未投）',
  attempts   INT NOT NULL DEFAULT 0 COMMENT '已投递尝试次数（含失败）',
  last_error VARCHAR(512) NOT NULL DEFAULT '' COMMENT '最近一次投递错误',
  UNIQUE KEY uk_event_key (event_key),
  KEY idx_unsent (sent_at, attempts, id)  -- relay 轮询：未投 + 未超限
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- audit：event-consumer 写审计明细（billing.events 流的下游）。唯一键 uk_event_id = 幂等锚：
-- at-least-once 消费重投同一事件时 INSERT 撞唯一索引收敛成 no-op，等价 exactly-once 落库。
CREATE TABLE IF NOT EXISTS audit_events (
  id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  event_id          VARCHAR(128) NOT NULL COMMENT '= outbox.event_key，幂等锚',
  tenant_id         BIGINT UNSIGNED NOT NULL,
  request_id        VARCHAR(64) NOT NULL,
  phase             VARCHAR(16) NOT NULL COMMENT 'reserve|usage|settle|reverse',
  amount_cents      BIGINT NOT NULL COMMENT '本次金额变动（分）：reserve 预扣 / settle delta / reverse 退款(-pre)；usage 为 0',
  model             VARCHAR(64) NOT NULL DEFAULT '',
  funding           VARCHAR(16) NOT NULL DEFAULT 'balance' COMMENT 'balance|subscription',
  prompt_tokens     INT NULL,
  completion_tokens INT NULL,
  payload           JSON NULL COMMENT '原始事件体',
  source            VARCHAR(16) NOT NULL DEFAULT 'kafka' COMMENT '事件来源：kafka|dlq',
  kafka_partition   INT NOT NULL DEFAULT 0,
  kafka_offset      BIGINT NOT NULL DEFAULT 0,
  consumed_at       DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_event_id (event_id),
  KEY idx_tenant_phase (tenant_id, phase, consumed_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ---------- 管理员（newapi 式初始登录） ----------
-- admin_users：管理员账号，密码 bcrypt 哈希，非空则视为已初始化（前端走登录而非初始化）。
CREATE TABLE IF NOT EXISTS admin_users (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  username      VARCHAR(64) NOT NULL COMMENT '管理员用户名',
  password_hash VARCHAR(255) NOT NULL COMMENT 'bcrypt 哈希，绝不回显',
  status        TINYINT NOT NULL DEFAULT 0 COMMENT '0=active 1=disabled',
  created_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_admin_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- admin_sessions：登录会话（HttpOnly cookie 载体，随机 token）。
CREATE TABLE IF NOT EXISTS admin_sessions (
  id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  token      VARCHAR(64) NOT NULL COMMENT '会话 token（随机串，cookie 值）',
  username   VARCHAR(64) NOT NULL,
  expires_at DATETIME(3) NOT NULL COMMENT '过期时间',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_admin_token (token)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ---------- 种子数据 ----------

-- 普通用户独立租户；网页会话关联限时 api_keys，仅保存 token 的 SHA-256。
CREATE TABLE IF NOT EXISTS portal_users (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  username VARCHAR(64) NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  tenant_id BIGINT UNSIGNED NOT NULL,
  status TINYINT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY idx_portal_users_username (username),
  UNIQUE KEY idx_portal_users_tenant_id (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS portal_sessions (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  api_key_id BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_portal_sessions_user_id (user_id),
  UNIQUE KEY idx_portal_sessions_api_key_id (api_key_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 演示租户：初始额度 5000 分 = ¥50（billing 服务启动时会据此补建 Redis balance key）。
INSERT INTO tenants (name, initial_quota) VALUES ('demo', 5000);

-- 演示 API Key（明文 sk-demo-8f3a2b1c9d4e5f60，永不落库，只存 SHA-256 + 前 8 位明文前缀）。
-- key_hash = SHA-256('sk-demo-8f3a2b1c9d4e5f60')
INSERT INTO api_keys (tenant_id, key_hash, key_prefix, name) VALUES
  (1, 'd0cc753715f6e0aa383534c467f56d49ad19474c2c5adf6c7e6ef02eb842c5b2', 'sk-demo-8', 'demo-key');

-- 两个 mock provider（M2 起由 router 服务读取；权重在 M5 做故障注入）。
-- models=["*"]：通配全部目录模型（目录与路由解耦——新增 SKU 无需改 provider）。
-- 注意：M2 用宿主原生进程跑，base_url 指向 127.0.0.1；M5 容器化部署时改为服务名 mock-provider。
INSERT INTO providers (name, base_url, models, weight) VALUES
  ('mock-a', 'http://127.0.0.1:9201', '["*"]', 5),
  ('mock-b', 'http://127.0.0.1:9202', '["*"]', 3);
