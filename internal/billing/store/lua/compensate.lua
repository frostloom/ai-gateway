-- 补偿预扣：仅当 guard 的值是本次调用 owner 时，删除 guard 并加回预扣额。
-- 插入失败后，调用方必须先持有该请求的 MySQL 会话锁并确认账本无 reserve 行。
-- 即使恢复会话也失联，后续预占 Lua 会接管 owner，旧调用不能再退款。
--
-- KEYS[1] = billing:balance:{tenant_id}
-- KEYS[2] = billing:req:{request_id}
-- ARGV[1] = pre_quota
-- ARGV[2] = 唯一调用 owner
--
-- return 1=已补偿, 0=guard 不匹配或已不存在
local cur = redis.call('GET', KEYS[2])
if cur ~= ARGV[2] then
  return 0
end
redis.call('DEL', KEYS[2])
local n = tonumber(ARGV[1])
if n > 0 then
  redis.call('INCRBY', KEYS[1], n)
end
return 1
