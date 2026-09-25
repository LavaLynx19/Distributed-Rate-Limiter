-- Resolve an API key to its tenant record in one round-trip.
-- KEYS[1] = SHA-256 hex of the presented key (raw keys are never stored)
--
-- Returns {} if the key or its tenant is unknown, otherwise
-- { tenant, plan, keys, mode }. See ARCHITECTURE.md section 10.
--
-- NOTE: like sliding_window.lua, this builds keys from KEYS[1]; in a Redis
-- Cluster they would need hash tags to share a slot.

local tenant = redis.call('HGET', 'apikey::' .. KEYS[1], 'tenant')
if not tenant then
  return {}
end

local record = redis.call('HMGET', 'tenant::' .. tenant, 'plan', 'keys', 'mode')
if not record[1] then
  return {} -- key points at a deleted tenant: treat as unregistered
end

return { tenant, record[1], record[2] or '1', record[3] or 'pooled' }
