# syntax=docker/dockerfile:1

# Dependencies are pure JavaScript, so install them once on the build host's
# native platform and copy them into each target image (arm64 / amd64).
FROM --platform=$BUILDPLATFORM node:24-alpine AS deps
WORKDIR /app
RUN corepack enable
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --prod --frozen-lockfile

FROM node:24-alpine
WORKDIR /app
ENV NODE_ENV=production
COPY --from=deps /app/node_modules ./node_modules
COPY package.json ./
COPY src ./src
USER node
# Node 24 runs the TypeScript sources directly (type stripping); no build step.
CMD ["node", "src/main.ts"]
