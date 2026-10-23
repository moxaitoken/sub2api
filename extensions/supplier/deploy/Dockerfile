# syntax=docker/dockerfile:1.7
FROM node:24-alpine AS frontend
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@10.10.0 --activate
COPY extensions/supplier/frontend/package.json extensions/supplier/frontend/pnpm-lock.yaml ./
RUN --mount=type=cache,id=supplier-pnpm-store,target=/root/.local/share/pnpm/store pnpm install --frozen-lockfile
COPY extensions/supplier/frontend/ ./
RUN pnpm run build

FROM golang:1.27.2-alpine AS backend
WORKDIR /app
COPY extensions/supplier/backend/go.mod extensions/supplier/backend/go.sum ./
RUN --mount=type=cache,id=supplier-gomod,target=/go/pkg/mod go mod download
COPY extensions/supplier/backend/ ./
RUN --mount=type=cache,id=supplier-gomod,target=/go/pkg/mod --mount=type=cache,id=supplier-gobuild,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /supplier-server ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && addgroup -g 10001 supplier && adduser -D -u 10001 -G supplier supplier
ARG COMMIT
LABEL org.opencontainers.image.revision=$COMMIT
COPY --from=backend /supplier-server /app/supplier-server
COPY --from=frontend /app/dist /app/static
USER 10001:10001
ENV SUPPLIER_LISTEN=0.0.0.0:8091 SUPPLIER_STATIC_DIR=/app/static
EXPOSE 8091
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8091/health || exit 1
ENTRYPOINT ["/app/supplier-server"]
