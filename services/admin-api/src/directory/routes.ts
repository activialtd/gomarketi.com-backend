import type { FastifyInstance } from "fastify";
import { requireAuth } from "../auth/middleware.js";
import { getCustomer, getVendor, listCustomers, listVendors } from "./repo.js";

interface ListQuery {
  q?: string;
  page?: string;
  per_page?: string;
}

export async function directoryRoutes(app: FastifyInstance) {
  app.get<{ Querystring: ListQuery }>(
    "/v1/admin/customers",
    { preHandler: requireAuth },
    async (request) => {
      const { q, page, per_page } = request.query;
      return listCustomers({ q, page: page ? Number(page) : undefined, perPage: per_page ? Number(per_page) : undefined });
    },
  );

  app.get<{ Params: { id: string } }>(
    "/v1/admin/customers/:id",
    { preHandler: requireAuth },
    async (request, reply) => {
      const result = await getCustomer(request.params.id);
      if (!result) return reply.code(404).send({ error: "customer not found" });
      return result;
    },
  );

  app.get<{ Querystring: ListQuery }>(
    "/v1/admin/vendors",
    { preHandler: requireAuth },
    async (request) => {
      const { q, page, per_page } = request.query;
      return listVendors({ q, page: page ? Number(page) : undefined, perPage: per_page ? Number(per_page) : undefined });
    },
  );

  app.get<{ Params: { id: string } }>(
    "/v1/admin/vendors/:id",
    { preHandler: requireAuth },
    async (request, reply) => {
      const result = await getVendor(request.params.id);
      if (!result) return reply.code(404).send({ error: "vendor not found" });
      return result;
    },
  );
}
