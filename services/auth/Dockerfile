FROM golang:1.22-alpine AS builder

WORKDIR /repo

COPY shared/pkg/go.mod shared/pkg/go.sum ./shared/pkg/
COPY services/auth/go.mod services/auth/go.sum ./services/auth/

RUN cd services/auth && GOWORK=off go mod download

COPY shared/pkg/ ./shared/pkg/
COPY services/auth/ ./services/auth/

RUN cd services/auth && GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o /app/auth-service \
    ./cmd/server

FROM alpine:3.20

RUN addgroup -S app && adduser -S app -G app

COPY --from=builder /app/auth-service /app/auth-service

USER app
EXPOSE 8080

ENTRYPOINT ["/app/auth-service"]
