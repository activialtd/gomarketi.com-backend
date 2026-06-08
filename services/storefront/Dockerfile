# docker build -f services/storefront/Dockerfile .  (run from repo root)
FROM golang:1.22-alpine AS builder

WORKDIR /repo

COPY go.work go.work.sum ./
COPY shared/pkg/go.mod shared/pkg/go.sum ./shared/pkg/
COPY services/storefront/go.mod services/storefront/go.sum ./services/storefront/

RUN go work sync && \
    cd services/storefront && go mod download

COPY shared/pkg/ ./shared/pkg/
COPY services/storefront/ ./services/storefront/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o /app/storefront-service \
    ./services/storefront/cmd/server

FROM alpine:3.20

RUN addgroup -S app && adduser -S app -G app
COPY --from=builder /app/storefront-service /app/storefront-service

USER app
EXPOSE 8082

ENTRYPOINT ["/app/storefront-service"]
