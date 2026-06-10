FROM golang:1.22-alpine AS builder

WORKDIR /repo

COPY shared/pkg/go.mod shared/pkg/go.sum ./shared/pkg/
COPY services/gateway/go.mod services/gateway/go.sum ./services/gateway/

RUN cd services/gateway && GOWORK=off go mod download

COPY shared/pkg/ ./shared/pkg/
COPY services/gateway/ ./services/gateway/

RUN cd services/gateway && GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o /app/gateway \
    ./cmd/server

FROM alpine:3.20

RUN addgroup -S app && adduser -S app -G app

COPY --from=builder /app/gateway /app/gateway

USER app
EXPOSE 8080

ENTRYPOINT ["/app/gateway"]
