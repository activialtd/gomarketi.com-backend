import Fastify from "fastify";
import cors from "@fastify/cors";
import { config } from "./config.js";
import { healthRoutes } from "./health/routes.js";
import { authRoutes } from "./auth/routes.js";
import { directoryRoutes } from "./directory/routes.js";
import { batchRoutes } from "./batches/routes.js";

const app = Fastify({
  logger: {
    transport: config.env === "development" ? { target: "pino-pretty" } : undefined,
  },
});

await app.register(cors, {
  origin: config.allowedOrigins.length > 0 ? config.allowedOrigins : true,
});

await app.register(healthRoutes);
await app.register(authRoutes);
await app.register(directoryRoutes);
await app.register(batchRoutes);

try {
  await app.listen({ port: config.port, host: "0.0.0.0" });
} catch (err) {
  app.log.error(err);
  process.exit(1);
}
