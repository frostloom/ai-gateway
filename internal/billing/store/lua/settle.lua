-- 结算/退款：把 delta 恰好在 Redis 余额上应用一次（幂等）。
-- delta 语义：新余额 = 旧余额 - delta
--   settle 正 delta 补扣；负 delta 退回
--   reverse 用负 delta（-pre）全退
-- 补扣超过当前余额时只扣到 0，不把余额打成负数。调用方据此结清账单并记下短欠，
-- 避免「补扣被拒绝 → 账单永远 pending → 对账反复失败」。
--
-- KEYS[1] = billing:balance:{tenant_id}
-- KEYS[2] = billing:delta:{request_id}  值 = 实际应用的 delta
-- ARGV[1] = 请求的 delta
--
-- return {code, balance, applied}
--   code: 1=按请求额应用, 2=补扣不足只扣到 0, 0=已应用过
--   applied: 本次（或首次）实际应用的 delta
local bal = tonumber(redis.call('GET', KEYS[1]) or '0')
local existing = redis.call('GET', KEYS[2])
if existing then
  return {0, bal, tonumber(existing)}
end
local delta = tonumber(ARGV[1])
local applied = delta
if delta > 0 and bal < delta then
  applied = bal
  if applied < 0 then
    applied = 0
  end
end
redis.call('SET', KEYS[2], tostring(applied), 'EX', 86400)
if applied > 0 then
  redis.call('DECRBY', KEYS[1], applied)
elseif applied < 0 then
  redis.call('INCRBY', KEYS[1], -applied)
end
local code = 1
if delta > 0 and applied < delta then
  code = 2
end
return {code, bal - applied, applied}
