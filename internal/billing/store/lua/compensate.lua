-- 补偿预扣：仅当 guard 的值就是本次 request_id 时，删除 guard 并加回预扣额。
-- 插入 reserve 行失败后调用。guard 不匹配说明这笔扣减不属于本次调用，不能动余额。
--
-- KEYS[1] = billing:balance:{tenant_id}
-- KEYS[2] = billing:req:{request_id}
-- ARGV[1] = pre_quota
-- ARGV[2] = request_id
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
