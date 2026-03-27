-- Leaky Bucket — Atomic Rate Limit Check
-- KEYS[1] = identifier (user_id or ip_address)
-- ARGV[1] = capacity (max bucket size)
-- ARGV[2] = leak_rate (requests drained per second)
--
-- Redis Hash: leaky_bucket::{identifier}
--   fields: water_level (float), last_leak (float timestamp)

-- STEP 1: Get Redis server time with sub-second precision
local time = redis.call('TIME')
local now = tonumber(time[1]) + tonumber(time[2]) / 1e6

local capacity = tonumber(ARGV[1])
local leak_rate = tonumber(ARGV[2])
local key = "leaky_bucket::" .. KEYS[1]

-- STEP 2: Read current state
local state = redis.call('HMGET', key, 'water_level', 'last_leak')
local water_level = tonumber(state[1])
local last_leak = tonumber(state[2])

-- STEP 3: Initialize on first request (empty bucket)
if water_level == nil then
  water_level = 0
  last_leak = now
end

-- STEP 4: Drain water based on elapsed time
local elapsed = now - last_leak
local drained = elapsed * leak_rate
water_level = math.max(0, water_level - drained)

-- STEP 5: Try to add 1 unit of water
if water_level + 1 > capacity then
  -- Bucket overflow — calculate retry delay
  local retry_after = math.ceil((water_level + 1 - capacity) / leak_rate)
  return { "BLOCKED", 0, retry_after }
end

-- STEP 6: Add water, update state
water_level = water_level + 1
redis.call('HMSET', key, 'water_level', tostring(water_level), 'last_leak', tostring(now))

-- Set TTL for garbage collection: time to fully drain bucket
local ttl = math.ceil(capacity / leak_rate)
redis.call('EXPIRE', key, ttl)

-- STEP 7: Return success
local remaining = math.floor(capacity - water_level)
local reset_ttl = math.ceil(water_level / leak_rate)
return { "ALLOWED", remaining, reset_ttl }
