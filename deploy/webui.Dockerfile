FROM node:22-bookworm-slim AS build
WORKDIR /src
RUN corepack enable && corepack prepare pnpm@9.15.0 --activate
COPY upstream/twilight/webui/package.json upstream/twilight/webui/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY upstream/twilight/webui/ ./
ENV NEXT_TELEMETRY_DISABLED=1
RUN pnpm typecheck && pnpm build
FROM node:22-bookworm-slim
WORKDIR /app
COPY --from=build --chown=node:node /src/.next/standalone ./
COPY --from=build --chown=node:node /src/.next/static ./.next/static
COPY --from=build --chown=node:node /src/public ./public
USER node
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0 PORT=3000
CMD ["node","server.js"]
