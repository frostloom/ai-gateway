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
-- Lua 5.1 numbers are doubles, and tostring only retains limited digits.
-- Restrict all arithmetic and persisted delta markers to exact 14-digit integers.
local function safe_int(raw, signed)
  if not raw or not string.match(raw, '^%-?%d+$') then return nil end
  local negative = string.sub(raw, 1, 1) == '-'
  if negative and not signed then return nil end
  local digits = negative and string.sub(raw, 2) or raw
  digits = string.gsub(digits, '^0+', '')
  if digits == '' then digits = '0' end
  if #digits > 14 then return nil end
  return tonumber(raw)
end
local cur = redis.call('GET', KEYS[2])
if cur ~= ARGV[2] then
  return 0
end
local n = safe_int(ARGV[1], false)
local bal = safe_int(redis.call('GET', KEYS[1]) or '0', false)
if not n or not bal or bal + n > 99999999999999 then return redis.error_reply('cash amount outside safe range') end
redis.call('DEL', KEYS[2])
if n > 0 then
  redis.call('INCRBY', KEYS[1], n)
end
return 1
