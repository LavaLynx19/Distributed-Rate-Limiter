# Load generator for the bench profile. Build from the repo root:
#   docker build -f bench/Dockerfile .
#
# autocannon comes from node/package-lock.json, so it is the exact version
# used for the native benchmarks. wrk is multi-threaded C, added because
# single-threaded autocannon capped Go's loose-mode numbers in Phase 3.

FROM node:25-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends wrk curl redis-tools \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /bench
COPY node/package.json node/package-lock.json ./
RUN npm ci --include=dev && npm cache clean --force
ENV PATH="/bench/node_modules/.bin:${PATH}"

USER node
ENTRYPOINT []
CMD ["sleep", "infinity"]
