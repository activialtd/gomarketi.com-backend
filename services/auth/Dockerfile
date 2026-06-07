# Build stage — use the full repo as context so the go.work workspace resolves.
# docker build -f services/auth/Dockerfile .  (run from repo root)
FROM golang:1.22-alpine AS builder

WORKDIR /repo

# Copy go.work and workspace-level sum file first to cache module graph.
COPY go.work go.work.sum ./

# Copy only the modules that auth depends on so Docker layer cache is efficient.
COPY shared/pkg/go.mod shared/pkg/go.sum ./shared/pkg/
COPY services/auth/go.mod services/auth/go.sum ./services/auth/

# Download dependencies (cached unless go.mod/go.sum change).
RUN go work sync && \
    cd services/auth && go mod download

# Copy all source needed to build.
COPY shared/pkg/ ./shared/pkg/
COPY services/auth/ ./services/auth/

# Build a statically linked binary.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o /app/auth-service \
    ./services/auth/cmd/server

# Runtime stage — minimal image.
FROM alpine:3.20

RUN addgroup -S app && adduser -S app -G app

COPY --from=builder /app/auth-service /app/auth-service

USER app
EXPOSE 8080

ENTRYPOINT ["/app/auth-service"]
