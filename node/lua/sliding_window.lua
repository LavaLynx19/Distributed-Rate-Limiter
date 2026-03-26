-- Sliding Window Counter — Atomic Rate Limit Check
-- KEYS[1] = identifier (user_id or ip_address)
-- ARGV[1] = MAX_LIMIT (e.g. 100)
-- ARGV[2] = batch_count (1 for Strict, N for Loose)
--
-- NOTE: This script constructs 5 internal keys from KEYS[1].
-- In a Redis Cluster, these keys may hash to different slots.
-- For cluster support, use hash tags: rate_limit::{identifier}::segment

-- STEP 1: Get Redis server time (authoritative clock — never use local time)
local time = redis.call('TIME')
local current_time = tonumber(time[1])
local current_segment = math.floor(current_time / 60) * 60

-- STEP 2: Build 5 segment keys (current + 4 prior minutes)
local key_prefix = "rate_limit::" .. KEYS[1] .. "::"
local keys = {}
for i = 0, 4 do
  keys[i + 1] = key_prefix .. (current_segment - i * 60)
end

-- STEP 3: Fetch all counts atomically
local counts = redis.call('MGET', keys[1], keys[2], keys[3], keys[4], keys[5])

-- STEP 4: Sum rolling usage
local total_usage = 0
for i = 1, 5 do
  total_usage = total_usage + (tonumber(counts[i]) or 0)
end

-- STEP 5: Enforce limit
local max_limit = tonumber(ARGV[1])
local batch_count = tonumber(ARGV[2])

if (total_usage + batch_count) > max_limit then
  local oldest_ttl = redis.call('TTL', keys[5])
  if oldest_ttl < 0 then oldest_ttl = 60 end
  return { "BLOCKED", 0, oldest_ttl }
end

-- STEP 6: Increment current segment
local exists = redis.call('EXISTS', keys[1])
if exists == 0 then
  redis.call('SETEX', keys[1], 300, batch_count)
else
  redis.call('INCRBY', keys[1], batch_count)
end

-- STEP 7: Return success
local remaining = max_limit - (total_usage + batch_count)
local oldest_ttl = redis.call('TTL', keys[5])
if oldest_ttl < 0 then oldest_ttl = 60 end
return { "ALLOWED", remaining, oldest_ttl }
