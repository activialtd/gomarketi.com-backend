# docker build -f services/catalogue/Dockerfile .  (run from repo root)
FROM golang:1.22-alpine AS builder

WORKDIR /repo

COPY go.work go.work.sum ./
COPY shared/pkg/go.mod shared/pkg/go.sum ./shared/pkg/
COPY services/catalogue/go.mod services/catalogue/go.sum ./services/catalogue/

RUN go work sync && \
    cd services/catalogue && go mod download

COPY shared/pkg/ ./shared/pkg/
COPY services/catalogue/ ./services/catalogue/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o /app/catalogue-service \
    ./services/catalogue/cmd/server

FROM alpine:3.20

RUN addgroup -S app && adduser -S app -G app
COPY --from=builder /app/catalogue-service /app/catalogue-service

USER app
EXPOSE 8083

ENTRYPOINT ["/app/catalogue-service"]
