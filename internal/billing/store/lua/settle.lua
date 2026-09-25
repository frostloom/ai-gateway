-- 结算/退款：把 delta 恰好在 Redis 余额上应用一次（幂等）。
-- 与 reserve.lua 的区别：这里不是「预占扣减」，而是对预占结果的修正——
--   delta 统一语义：新余额 = 旧余额 - delta
--     settle 正 delta 补扣（实际>预扣，再扣差额）、负 delta 退回（实际<预扣，退差额）
--     reverse 用负 delta（-pre）全退（预扣的 pre 原路退回）
-- 应用「恰好一次」靠 billing:delta:{request_id} guard（Settle 与 Reverse 共用，
-- 二者互斥：谁先拿到 guard，另一个就变 no-op）。
--
-- KEYS[1] = billing:balance:{tenant_id}   Int
-- KEYS[2] = billing:delta:{request_id}    String delta guard（NX 写入，TTL 24h）
-- ARGV[1] = delta（正=补扣，负=退回）
--
-- return {code, balance}
--   code: 1=已应用, 0=已应用过（幂等重放）, -1=透支拒绝（仅补扣时检查）
--   balance: 应用后的余额
local bal = tonumber(redis.call('GET', KEYS[1]) or '0')
if redis.call('EXISTS', KEYS[2]) == 1 then
  return {0, bal}
end
-- 只有「补扣」才可能透支：余额必须 ≥ 本次补扣额。退回（负 delta）只会增加，无需检查。
if tonumber(ARGV[1]) > 0 and bal < tonumber(ARGV[1]) then
  return {-1, bal}
end
redis.call('SET', KEYS[2], '1', 'EX', 86400)
if tonumber(ARGV[1]) > 0 then
  redis.call('DECRBY', KEYS[1], ARGV[1])
elseif tonumber(ARGV[1]) < 0 then
  redis.call('INCRBY', KEYS[1], -tonumber(ARGV[1]))
end
return {1, bal - tonumber(ARGV[1])}
