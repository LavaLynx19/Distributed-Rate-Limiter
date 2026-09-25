-- Sliding Window Counter — Atomic Rate Limit Check
-- KEYS[1] = identifier (user_id or ip_address)
-- ARGV[1] = MAX_LIMIT (e.g. 100)
-- ARGV[2] = batch_count (1 for Strict, N for a Loose write-back, 0 to refresh)
-- ARGV[3] = mode: "check" (default) or "writeback"
--   check      the request(s) have not been served yet: an over-limit batch is
--              rejected and NOT recorded (strict mode)
--   writeback  the request(s) were already served by a gateway's local
--              buffer: the batch is ALWAYS recorded so Redis's count matches
--              reality, and the result reports the remaining capacity
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

local max_limit = tonumber(ARGV[1])
local batch_count = tonumber(ARGV[2])
local writeback = ARGV[3] == "writeback"

-- A segment starting at S stays in the window until S + 300. Key TTLs are not
-- used: they count from a segment's first write, overstating by up to 60s.
local function exit_after(i)
  return (current_segment - (i - 1) * 60 + 300) - current_time
end

-- Seconds until the oldest segment with any usage leaves the window, i.e.
-- the next moment capacity frees up (X-RateLimit-Reset).
local function reset_after()
  for i = 5, 2, -1 do
    if (tonumber(counts[i]) or 0) > 0 then return exit_after(i) end
  end
  return exit_after(1)
end

-- Seconds until enough usage leaves the window to free `need` slots,
-- walking segments oldest-first (Retry-After).
local function retry_after(need)
  local freed = 0
  for i = 5, 1, -1 do
    freed = freed + (tonumber(counts[i]) or 0)
    if freed >= need then return math.max(1, exit_after(i)) end
  end
  return 300 -- a batch larger than the limit can never fit
end

local function record(count)
  if count <= 0 then return end
  if redis.call('EXISTS', keys[1]) == 0 then
    redis.call('SETEX', keys[1], 300, count)
  else
    redis.call('INCRBY', keys[1], count)
  end
end

-- STEP 5 (writeback): record unconditionally, then report what is left
if writeback then
  record(batch_count)
  -- retry_after must see the batch just recorded in the current segment.
  counts[1] = (tonumber(counts[1]) or 0) + batch_count
  local remaining = max_limit - (total_usage + batch_count)
  if remaining <= 0 then
    -- Room for one more request once enough usage has left the window.
    return { "BLOCKED", 0, retry_after(1 - remaining) }
  end
  return { "ALLOWED", remaining, reset_after() }
end

-- STEP 5 (check): enforce limit before recording
local shortfall = (total_usage + batch_count) - max_limit
if shortfall > 0 then
  return { "BLOCKED", 0, retry_after(shortfall) }
end

-- STEP 6: Increment current segment
record(batch_count)

-- STEP 7: Return success
local remaining = max_limit - (total_usage + batch_count)
return { "ALLOWED", remaining, reset_after() }
