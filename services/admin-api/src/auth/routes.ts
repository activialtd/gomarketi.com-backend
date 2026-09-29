import type { FastifyInstance } from "fastify";
import bcrypt from "bcryptjs";
import { db } from "../db/pool.js";
import { issueAdminToken } from "./jwt.js";
import { requireAuth } from "./middleware.js";

interface LoginBody {
  email: string;
  password: string;
}

export async function authRoutes(app: FastifyInstance) {
  app.post<{ Body: LoginBody }>("/v1/admin/auth/login", async (request, reply) => {
    const { email, password } = request.body ?? {};
    if (!email || !password) {
      return reply.code(400).send({ error: "email and password are required" });
    }

    const user = await db
      .selectFrom("admin_users")
      .selectAll()
      .where("email", "=", email.toLowerCase())
      .executeTakeFirst();

    // Same response for "no such user" and "wrong password" — don't leak
    // which one it was.
    if (!user || !user.is_active) {
      return reply.code(401).send({ error: "invalid credentials" });
    }
    const valid = await bcrypt.compare(password, user.password_hash);
    if (!valid) {
      return reply.code(401).send({ error: "invalid credentials" });
    }

    await db
      .updateTable("admin_users")
      .set({ last_login_at: new Date(), updated_at: new Date() })
      .where("id", "=", user.id)
      .execute();

    const token = issueAdminToken({
      id: user.id,
      email: user.email,
      fullName: user.full_name,
      role: user.role,
    });

    return reply.send({
      token,
      admin: { id: user.id, email: user.email, full_name: user.full_name, role: user.role },
    });
  });

  app.get("/v1/admin/auth/me", { preHandler: requireAuth }, async (request) => {
    return { admin: request.admin };
  });
}
