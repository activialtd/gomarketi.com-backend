import "dotenv/config";
import { readFileSync } from "node:fs";

function required(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}

// Accepts either a file path (JWT_PRIVATE_KEY_PATH, matching the Go auth
// service's local-dev convention) or a base64-encoded PEM blob
// (JWT_PRIVATE_KEY_B64, matching the SSM-backed convention every other
// service uses in staging/production) — mirrors how the Go side supports
// both file-path and env-var key material.
function loadKey(pathEnv: string, b64Env: string): string {
  const b64 = process.env[b64Env];
  if (b64) return Buffer.from(b64, "base64").toString("utf8");
  const path = process.env[pathEnv];
  if (path) return readFileSync(path, "utf8");
  throw new Error(`Neither ${b64Env} nor ${pathEnv} is set`);
}

export const config = {
  env: process.env.ENV ?? "development",
  port: Number(process.env.PORT ?? 8086),
  databaseUrl: required("DATABASE_URL"),
  allowedOrigins: (process.env.ALLOWED_ORIGINS ?? "").split(",").filter(Boolean),
  jwt: {
    privateKey: loadKey("JWT_PRIVATE_KEY_PATH", "JWT_PRIVATE_KEY_B64"),
    publicKey: loadKey("JWT_PUBLIC_KEY_PATH", "JWT_PUBLIC_KEY_B64"),
    accessTokenTTLSeconds: Number(process.env.JWT_ACCESS_TTL_SECONDS ?? 3600),
  },
  // The one internal service-to-service call this app makes — batch
  // dispatch calling orders service's no-show-refund endpoint directly
  // (not through the gateway). Empty key matches orders service's own
  // dev-mode "unprotected with a warning" fallback.
  ordersInternalUrl: process.env.ORDERS_INTERNAL_URL ?? "http://localhost:8084",
  internalApiKey: process.env.INTERNAL_API_KEY ?? "",
};
