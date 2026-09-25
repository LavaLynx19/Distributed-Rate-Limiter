#!/usr/bin/env bash
# Isolated, CPU-pinned admit-path benchmark (ARCHITECTURE.md section 9).
#
#   bench/docker-bench.sh [reps]        default: 3 interleaved repetitions
#
# Runs one gateway at a time against its own Redis, with every request
# admitted (RATE_LIMIT_MAX=1e8) so both gateways do the full amount of work.
# Each configuration is measured with autocannon (continuity with the native
# results) and wrk (multi-threaded, to find the ceiling), both anonymously
# (IP identity) and with a registered API key (registry lookup served from
# the gateway's cache; ARCHITECTURE.md section 10). Raw outputs and a summary
# land in bench/results/<timestamp>/.
set -euo pipefail

cd "$(dirname "$0")/.."
REPS="${1:-3}"
DURATION=10
OUT="bench/results/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$OUT"

compose() { docker compose --profile bench "$@"; }
loadgen() { compose exec -T loadgen "$@"; }

if docker compose ps --status running --services 2>/dev/null | grep -qxE 'redis|node|go|nginx'; then
  echo "warning: the default stack is running and will compete for CPU; stop it with 'docker compose stop' for clean numbers" >&2
fi

echo "building images..."
compose build --quiet node-bench go-bench loadgen
compose up -d --wait redis-bench loadgen >/dev/null

# CPU% (100 = one vCPU) for the three pinned containers, sampled mid-run.
sample_cpu() {
  sleep $((DURATION / 2))
  docker stats --no-stream --format '{{.Name}} {{.CPUPerc}}' \
    rate-limiter-redis-bench-1 "rate-limiter-$1-bench-1" rate-limiter-loadgen-1 \
    > "$2"
}

# Provision a tenant on the 'bench' plan (limit = BENCH_LIMIT) with keyctl
# from the Go image. FLUSHALL between runs would wipe it, so runs clear only
# rate-limit keys instead.
keyctl() { compose run --rm --no-deps -T --entrypoint /app/keyctl go-bench "$@"; }
clear_limits() {
  compose exec -T redis-bench sh -c "redis-cli --scan --pattern 'rate_limit::*' | xargs -r redis-cli DEL" >/dev/null
}
compose exec -T redis-bench redis-cli FLUSHALL >/dev/null
keyctl tenant set bench -plan bench -keys 1 -mode pooled >/dev/null
BENCH_KEY=$(keyctl key create bench 2>/dev/null | tr -d '\r')
if [[ ! "$BENCH_KEY" =~ ^rlk_[0-9A-Za-z]{32}$ ]]; then
  echo "error: could not provision the bench API key (keyed runs would silently be anonymous)" >&2
  exit 1
fi

run() { # impl host:port tool mode ident rep
  local impl=$1 target=$2 tool=$3 mode=$4 ident=$5 rep=$6
  local path="/api/$mode/resource" tag="$impl-$tool-$mode-$ident-$rep"
  local header=(); [ "$ident" = keyed ] && header=(-H "x-api-key: $BENCH_KEY")
  clear_limits
  sample_cpu "$impl" "$OUT/$tag.cpu" &
  if [ "$tool" = autocannon ]; then
    # Same shape as the native runs: 50 connections, pipelined x10 for loose.
    local pipe=1; [ "$mode" = loose ] && pipe=10
    loadgen autocannon -c 50 -p "$pipe" -d "$DURATION" ${header[@]+"${header[@]}"} -j "http://$target$path" > "$OUT/$tag.json" 2>/dev/null
  else
    loadgen wrk -t 8 -c 200 -d "${DURATION}s" ${header[@]+"${header[@]}"} --latency "http://$target$path" > "$OUT/$tag.txt"
  fi
  wait
}

for rep in $(seq 1 "$REPS"); do
  for impl in node go; do
    port=3000; [ "$impl" = go ] && port=3001
    compose up -d "$impl-bench" >/dev/null
    until loadgen curl -fs "http://$impl-bench:$port/api/open/health" >/dev/null 2>&1; do sleep 0.3; done

    for ident in anon keyed; do
      for tool in autocannon wrk; do
        for mode in strict loose; do
          run "$impl" "$impl-bench:$port" "$tool" "$mode" "$ident" "$rep"
        done
      done
    done

    compose stop "$impl-bench" >/dev/null
    echo "rep $rep/$REPS: $impl done"
  done
done

compose stop redis-bench loadgen >/dev/null
python3 bench/summarize.py "$OUT" | tee "$OUT/summary.md"
