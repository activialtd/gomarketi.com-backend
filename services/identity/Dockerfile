# docker build -f services/identity/Dockerfile .  (run from repo root)
FROM golang:1.22-alpine AS builder

WORKDIR /repo

COPY go.work go.work.sum ./
COPY shared/pkg/go.mod shared/pkg/go.sum ./shared/pkg/
COPY services/identity/go.mod services/identity/go.sum ./services/identity/

RUN go work sync && \
    cd services/identity && go mod download

COPY shared/pkg/ ./shared/pkg/
COPY services/identity/ ./services/identity/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o /app/identity-service \
    ./services/identity/cmd/server

FROM alpine:3.20

RUN addgroup -S app && adduser -S app -G app
COPY --from=builder /app/identity-service /app/identity-service

USER app
EXPOSE 8081

ENTRYPOINT ["/app/identity-service"]
