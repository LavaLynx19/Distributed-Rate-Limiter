// Loose-mode write-back buffer (ARCHITECTURE.md section 6), free of I/O so it
// can be unit-tested: the Redis write-back, the settings and the clock are
// injected. lib/heap-buffer.js wires the real ones.
//
// identifier -> { limit, pending, allowance, lastSync, lastFlush, retryAt, inFlight }
//   limit      the identity's sliding-window limit (plans differ per tenant)
//   pending    admitted requests not yet written to Redis
//   allowance  how many more requests this instance may admit before it hears
//              from Redis again; each write-back resets it to the cluster-wide
//              remaining capacity, so instances learn about each other's usage
//   lastSync   last successful write-back; if none lands for a whole window
//              (Redis down) the allowance resets to the full limit (fail-open)
//   lastFlush  last write-back attempt; paces refreshes and pruning
//   retryAt    when the last BLOCKED write-back said capacity frees up (ms),
//              or 0 if the last write-back wasn't BLOCKED
//   inFlight   a write-back for this identifier is awaiting Redis
//
// writeBack(identifier, limit, batch) -> { allowed, remaining, resetTtl, failedOpen }
// settings: { windowMs, batchThreshold, refreshIntervalMs, flushIntervalMs }
export function createBuffer({ writeBack, settings, now = () => Date.now() }) {
  const buffer = new Map();
  let flushInterval = null;

  // The pending count that triggers an immediate write-back. It shrinks for
  // small limits so the overshoot bound stays proportional.
  function threshold(limit) {
    return Math.max(1, Math.min(settings.batchThreshold, Math.floor(limit / 2)));
  }

  function admit(identifier, limit) {
    const t = now();
    let entry = buffer.get(identifier);
    if (!entry) {
      // Cluster usage is unknown until the first sync, so start optimistic;
      // the first write-back (at most one batch away) corrects it.
      entry = { limit, pending: 0, allowance: limit, lastSync: t, lastFlush: t, retryAt: 0, inFlight: null };
      buffer.set(identifier, entry);
    } else if (entry.limit !== limit) {
      // A plan change: shift the allowance by the difference, don't start over.
      entry.allowance += limit - entry.limit;
      entry.limit = limit;
    }

    if (t - entry.lastSync > settings.windowMs) {
      entry.lastSync = t;
      entry.allowance = entry.limit;
    }

    // Rejected requests are not counted: charging them to Redis would let a
    // blocked client burn its own future quota.
    if (entry.allowance <= 0) {
      // Precise wait from the last BLOCKED write-back; otherwise 1s, since
      // the next write-back (at most one tick away) will know more.
      const retryAfter = entry.retryAt > t ? Math.ceil((entry.retryAt - t) / 1000) : 1;
      return { admitted: false, remaining: 0, retryAfter };
    }

    entry.allowance -= 1;
    entry.pending += 1;

    if (entry.pending >= threshold(entry.limit)) {
      flushIdentifier(identifier, entry);
    }

    return { admitted: true, remaining: entry.allowance };
  }

  async function writeBatch(identifier, entry, batch) {
    try {
      const result = await writeBack(identifier, entry.limit, batch);
      if (result.failedOpen) {
        // Redis never recorded this batch; keep it for the next attempt
        // instead of silently dropping requests that were already served.
        entry.pending += batch;
        return;
      }
      // Requests admitted while awaiting Redis are exactly the current
      // pending count, and Redis's figure doesn't include them yet.
      const t = now();
      entry.allowance = result.remaining - entry.pending;
      entry.lastSync = t;
      entry.retryAt = result.allowed ? 0 : t + result.resetTtl * 1000;
    } finally {
      entry.inFlight = null;
    }
  }

  // Records the pending batch in Redis and resets the allowance to the
  // cluster-wide remaining capacity. With nothing pending it is a refresh: an
  // exhausted identifier asks whether capacity has freed up. Returns the
  // in-flight promise, or null when there is nothing to do.
  function flushIdentifier(identifier, entry) {
    if (entry.inFlight) return entry.inFlight;

    const batch = entry.pending;
    if (batch === 0 && entry.allowance > 0) return null;

    // Take the batch before the async call so requests arriving mid-flight
    // land in the next batch.
    entry.pending = 0;
    entry.lastFlush = now();
    entry.inFlight = writeBatch(identifier, entry, batch);
    return entry.inFlight;
  }

  function flushAll() {
    const t = now();
    const flushes = [];

    for (const [identifier, entry] of buffer) {
      const sinceFlush = t - entry.lastFlush;
      let flush = null;

      if (entry.pending > 0) {
        flush = flushIdentifier(identifier, entry);
      } else if (entry.allowance <= 0) {
        if (sinceFlush >= settings.refreshIntervalMs) {
          flush = flushIdentifier(identifier, entry);
        }
      } else if (!entry.inFlight && sinceFlush > settings.windowMs) {
        buffer.delete(identifier);
      }

      if (flush) flushes.push(flush);
    }

    return flushes;
  }

  function startFlushLoop() {
    if (flushInterval) return;
    flushInterval = setInterval(flushAll, settings.flushIntervalMs);
    // Allow process to exit even if interval is active
    flushInterval.unref();
  }

  function stopFlushLoop() {
    if (flushInterval) {
      clearInterval(flushInterval);
      flushInterval = null;
    }
  }

  // drain writes every pending count to Redis before shutdown. It waits for
  // flushes already in flight first, since those block a new flush of the
  // same identifier.
  async function drain() {
    await Promise.all([...buffer.values()].map((entry) => entry.inFlight).filter(Boolean));
    await Promise.all(flushAll());
  }

  function stats() {
    return {
      identifiers: buffer.size,
      entries: [...buffer].map(([id, e]) => ({
        identifier: id,
        limit: e.limit,
        pendingCount: e.pending,
        allowance: e.allowance,
        blocked: e.allowance <= 0,
      })),
    };
  }

  return { admit, flushAll, startFlushLoop, stopFlushLoop, drain, stats };
}
