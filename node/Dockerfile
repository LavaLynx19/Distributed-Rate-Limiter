# Build from the repo root so the shared lua/ directory is in context:
#   docker build -f node/Dockerfile .
#
# node:25-slim (glibc) rather than alpine: musl's allocator is measurably
# slower under Node's allocation-heavy workload and would skew benchmarks.

FROM node:25-slim
ENV NODE_ENV=production

# redis-client.js resolves the scripts as ../../lua from /app/node/lib.
WORKDIR /app/node

COPY node/package.json node/package-lock.json ./
RUN npm ci --omit=dev && npm cache clean --force

COPY node/server.js ./
COPY node/lib/ ./lib/
COPY node/routes/ ./routes/
COPY lua/ /app/lua/

ENV PORT=3000
EXPOSE 3000
USER node
CMD ["node", "server.js"]
