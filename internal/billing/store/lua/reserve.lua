-- 原子预占：判断余额 ≥ 预扣额 再扣减（消灭 check-then-act 竞态）。
-- 修复 new-api 的缺陷：它的钱包扣减是「先查余额再扣」的 check-then-act，
-- 扣减语句没有 WHERE quota >= N 守卫，并发请求下可能超卖成负余额。
-- 这里把「判余额 + 扣减」放进单条 Lua，Redis 单线程执行天然原子。
--
-- 同一 request_id 幂等：guard 存在即视为已扣（返回 2），重试/并发不会重复扣。
--
-- KEYS[1] = billing:balance:{tenant_id}   Int  当前余额
-- KEYS[2] = billing:req:{request_id}     String 预占 guard（NX 写入，TTL 24h）
-- ARGV[1] = pre_quota                    预扣额度（整数）
-- ARGV[2] = request_id
--
-- return {code, balance}
--   code: 1=扣减成功, 2=已扣过（幂等重放）, -1=余额不足
--   balance: 操作后的余额
local bal = tonumber(redis.call('GET', KEYS[1]) or '0')
if redis.call('EXISTS', KEYS[2]) == 1 then
  return {2, bal}
end
if bal < tonumber(ARGV[1]) then
  return {-1, bal}
end
redis.call('SET', KEYS[2], ARGV[2], 'EX', 86400)
redis.call('DECRBY', KEYS[1], ARGV[1])
return {1, bal - tonumber(ARGV[1])}
