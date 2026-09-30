# syntax=docker/dockerfile:1

# The build output is platform-independent static files, so build once on the
# host's native platform and copy into a multi-arch Caddy image.
FROM --platform=$BUILDPLATFORM node:24-alpine AS build
WORKDIR /app
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

FROM caddy:2.11-alpine
COPY --from=build /app/build /srv
# The Caddyfile is mounted from deploy/ by docker-compose.yml.
