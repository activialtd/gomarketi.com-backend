import type { FastifyInstance } from "fastify";
import { requireAuth } from "../auth/middleware.js";
import { getError, listErrors, reportError, resolveError, type ReportErrorInput } from "./repo.js";

interface ListQuery {
  page?: string;
  per_page?: string;
  service?: string;
  resolved?: string;
}

export async function errorRoutes(app: FastifyInstance) {
  app.get<{ Querystring: ListQuery }>(
    "/v1/admin/errors",
    { preHandler: requireAuth },
    async (request) => {
      const { page, per_page, service, resolved } = request.query;
      return listErrors({
        page: page ? Number(page) : undefined,
        perPage: per_page ? Number(per_page) : undefined,
        service,
        resolved: resolved === undefined ? undefined : resolved === "true",
      });
    },
  );

  app.get<{ Params: { id: string } }>(
    "/v1/admin/errors/:id",
    { preHandler: requireAuth },
    async (request, reply) => {
      const row = await getError(request.params.id);
      if (!row) return reply.code(404).send({ error: "error event not found" });
      return row;
    },
  );

  app.post<{ Params: { id: string } }>(
    "/v1/admin/errors/:id/resolve",
    { preHandler: requireAuth },
    async (request, reply) => {
      try {
        await resolveError(request.params.id, request.admin!.sub);
        return { ok: true };
      } catch (err) {
        return reply.code(400).send({ error: err instanceof Error ? err.message : String(err) });
      }
    },
  );

  // Unauthenticated on purpose — this is called by vendor-web/consumer-app's
  // own crash handlers, which by definition may be running with no admin (or
  // any) session available. See repo.ts's field caps for the abuse mitigation
  // this relies on in place of proper rate limiting.
  app.post<{ Body: Partial<ReportErrorInput> }>("/v1/admin/errors/report", async (request, reply) => {
    const { service, message } = request.body ?? {};
    if (!service || typeof service !== "string" || !message || typeof message !== "string") {
      return reply.code(400).send({ error: "service and message are required" });
    }
    await reportError(request.body as ReportErrorInput);
    return { ok: true };
  });
}
