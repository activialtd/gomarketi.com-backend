FROM node:20-alpine AS builder

WORKDIR /repo/services/admin-api

COPY services/admin-api/package.json services/admin-api/package-lock.json ./
RUN npm ci

COPY services/admin-api/ ./
RUN npm run build

FROM node:20-alpine

RUN addgroup -S app && adduser -S app -G app

WORKDIR /app
COPY --from=builder /repo/services/admin-api/package.json /repo/services/admin-api/package-lock.json ./
RUN npm ci --omit=dev

COPY --from=builder /repo/services/admin-api/dist ./dist
COPY --from=builder /repo/services/admin-api/db ./db

USER app
EXPOSE 8085

CMD ["node", "dist/src/server.js"]
