FROM golang:1.22-alpine AS builder

WORKDIR /repo

COPY shared/pkg/go.mod shared/pkg/go.sum ./shared/pkg/
COPY services/orders/go.mod services/orders/go.sum ./services/orders/

RUN cd services/orders && GOWORK=off go mod download

COPY shared/pkg/ ./shared/pkg/
COPY services/orders/ ./services/orders/

RUN cd services/orders && GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o /app/orders-service \
    ./cmd/server

FROM alpine:3.20

RUN addgroup -S app && adduser -S app -G app

COPY --from=builder /app/orders-service /app/orders-service

USER app
EXPOSE 8084

ENTRYPOINT ["/app/orders-service"]
