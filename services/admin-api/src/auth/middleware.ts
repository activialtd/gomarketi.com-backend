import type { FastifyReply, FastifyRequest } from "fastify";
import { verifyAdminToken, type AdminClaims, type AdminRole } from "./jwt.js";

declare module "fastify" {
  interface FastifyRequest {
    admin?: AdminClaims;
  }
}

export async function requireAuth(request: FastifyRequest, reply: FastifyReply) {
  const header = request.headers.authorization;
  const token = header?.startsWith("Bearer ") ? header.slice("Bearer ".length) : undefined;
  if (!token) {
    return reply.code(401).send({ error: "missing bearer token" });
  }
  try {
    request.admin = verifyAdminToken(token);
  } catch {
    return reply.code(401).send({ error: "invalid or expired token" });
  }
}

export function requireRole(...roles: AdminRole[]) {
  return async (request: FastifyRequest, reply: FastifyReply) => {
    if (!request.admin) {
      return reply.code(401).send({ error: "not authenticated" });
    }
    if (!roles.includes(request.admin.admin_role)) {
      return reply.code(403).send({ error: "insufficient role" });
    }
  };
}
