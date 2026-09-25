FROM golang:1.25-alpine AS builder

WORKDIR /repo

COPY shared/pkg/go.mod shared/pkg/go.sum ./shared/pkg/
COPY services/storefront/go.mod services/storefront/go.sum ./services/storefront/

RUN cd services/storefront && GOWORK=off go mod download

COPY shared/pkg/ ./shared/pkg/
COPY services/storefront/ ./services/storefront/

RUN cd services/storefront && GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o /app/storefront-service \
    ./cmd/server

FROM alpine:3.20

RUN addgroup -S app && adduser -S app -G app

COPY --from=builder /app/storefront-service /app/storefront-service

USER app
EXPOSE 8082

ENTRYPOINT ["/app/storefront-service"]
