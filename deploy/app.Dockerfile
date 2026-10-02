# syntax=docker/dockerfile:1

# Pairpad app image for Dokploy: the Go server, which also serves the built
# web app (STATIC_DIR). Build context is the repository root:
#   docker buildx build -f deploy/app.Dockerfile .
# Both build stages run on the builder's native platform; only the copied
# outputs are architecture-specific, so arm64 (Oracle Ampere) needs no
# emulation.

FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml web/.npmrc ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build && pnpm check:bundle

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS server
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY server/ ./
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/server /server
COPY --from=web /web/build /srv/web
ENV STATIC_DIR=/srv/web
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
