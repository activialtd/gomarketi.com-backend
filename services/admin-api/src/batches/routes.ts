import type { FastifyInstance } from "fastify";
import { requireAuth, requireRole } from "../auth/middleware.js";
import { dispatchBatch, getBatch, hubIntake, listBatches, releaseEscrow } from "./repo.js";

interface ListQuery {
  page?: string;
  per_page?: string;
}

export async function batchRoutes(app: FastifyInstance) {
  app.get<{ Querystring: ListQuery }>(
    "/v1/admin/batches",
    { preHandler: requireAuth },
    async (request) => {
      const { page, per_page } = request.query;
      return listBatches({ page: page ? Number(page) : undefined, perPage: per_page ? Number(per_page) : undefined });
    },
  );

  app.get<{ Params: { paymentReference: string } }>(
    "/v1/admin/batches/:paymentReference",
    { preHandler: requireAuth },
    async (request, reply) => {
      const result = await getBatch(request.params.paymentReference);
      if (!result) return reply.code(404).send({ error: "batch not found" });
      return result;
    },
  );

  app.post<{ Params: { id: string } }>(
    "/v1/admin/orders/:id/hub-intake",
    { preHandler: requireAuth },
    async (request, reply) => {
      try {
        await hubIntake(request.params.id, request.admin!.sub);
        return { ok: true };
      } catch (err) {
        return reply.code(400).send({ error: err instanceof Error ? err.message : String(err) });
      }
    },
  );

  app.post<{ Params: { paymentReference: string } }>(
    "/v1/admin/batches/:paymentReference/dispatch",
    { preHandler: [requireAuth, requireRole("supervisor", "super_admin")] },
    async (request, reply) => {
      try {
        return await dispatchBatch(request.params.paymentReference);
      } catch (err) {
        return reply.code(400).send({ error: err instanceof Error ? err.message : String(err) });
      }
    },
  );

  app.post<{ Params: { id: string } }>(
    "/v1/admin/orders/:id/release-escrow",
    { preHandler: [requireAuth, requireRole("supervisor", "super_admin")] },
    async (request, reply) => {
      try {
        await releaseEscrow(request.params.id);
        return { ok: true };
      } catch (err) {
        return reply.code(400).send({ error: err instanceof Error ? err.message : String(err) });
      }
    },
  );
}
