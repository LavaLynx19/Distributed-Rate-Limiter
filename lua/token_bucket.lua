-- Token Bucket — Atomic Rate Limit Check
-- KEYS[1] = identifier (user_id or ip_address)
-- ARGV[1] = capacity (max tokens)
-- ARGV[2] = refill_rate (tokens per second)
--
-- Redis Hash: token_bucket::{identifier}
--   fields: tokens (float), last_refill (float timestamp)

-- STEP 1: Get Redis server time with sub-second precision
local time = redis.call('TIME')
local now = tonumber(time[1]) + tonumber(time[2]) / 1e6

local capacity = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local key = "token_bucket::" .. KEYS[1]

-- STEP 2: Read current state
local state = redis.call('HMGET', key, 'tokens', 'last_refill')
local tokens = tonumber(state[1])
local last_refill = tonumber(state[2])

-- STEP 3: Initialize on first request (full bucket)
if tokens == nil then
  tokens = capacity
  last_refill = now
end

-- STEP 4: Refill tokens based on elapsed time
local elapsed = now - last_refill
local new_tokens = math.min(capacity, tokens + elapsed * refill_rate)

-- STEP 5: Try to consume 1 token
if new_tokens < 1 then
  -- Not enough tokens — calculate retry delay
  local retry_after = math.ceil((1 - new_tokens) / refill_rate)
  return { "BLOCKED", 0, retry_after }
end

-- STEP 6: Consume token, update state
new_tokens = new_tokens - 1
redis.call('HMSET', key, 'tokens', tostring(new_tokens), 'last_refill', tostring(now))

-- Set TTL for garbage collection: time to fill bucket from empty
local ttl = math.ceil(capacity / refill_rate)
redis.call('EXPIRE', key, ttl)

-- STEP 7: Return success
local remaining = math.floor(new_tokens)
local reset_ttl = math.ceil((capacity - new_tokens) / refill_rate)
return { "ALLOWED", remaining, reset_ttl }
