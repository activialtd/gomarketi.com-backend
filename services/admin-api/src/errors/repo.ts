import { db } from "../db/pool.js";
import type { ErrorEventLevel } from "../db/types.js";

const DEFAULT_PER_PAGE = 30;

function clampPaging(page?: number, perPage?: number) {
  const p = Math.max(1, page ?? 1);
  const pp = Math.min(100, Math.max(1, perPage ?? DEFAULT_PER_PAGE));
  return { page: p, perPage: pp, offset: (p - 1) * pp };
}

export async function listErrors(opts: { page?: number; perPage?: number; service?: string; resolved?: boolean }) {
  const { page, perPage, offset } = clampPaging(opts.page, opts.perPage);

  let query = db.selectFrom("error_events").selectAll();
  let countQuery = db.selectFrom("error_events");

  // Default view is the live queue (unresolved) — pass resolved=true to see history.
  const resolved = opts.resolved ?? false;
  query = query.where("resolved", "=", resolved);
  countQuery = countQuery.where("resolved", "=", resolved);

  if (opts.service) {
    query = query.where("service", "=", opts.service);
    countQuery = countQuery.where("service", "=", opts.service);
  }

  const [rows, totalRow] = await Promise.all([
    query.orderBy("created_at", "desc").limit(perPage).offset(offset).execute(),
    countQuery.select((eb) => eb.fn.countAll().as("count")).executeTakeFirst(),
  ]);

  return {
    errors: rows,
    total: Number(totalRow?.count ?? 0),
    page,
    per_page: perPage,
  };
}

export async function getError(id: string) {
  return db.selectFrom("error_events").selectAll().where("id", "=", id).executeTakeFirst() ?? null;
}

export async function resolveError(id: string, adminId: string) {
  const result = await db
    .updateTable("error_events")
    .set({ resolved: true, resolved_at: new Date(), resolved_by: adminId })
    .where("id", "=", id)
    .where("resolved", "=", false)
    .executeTakeFirst();

  if (result.numUpdatedRows === 0n) {
    throw new Error("error event not found, or already resolved");
  }
}

// Field caps for the unauthenticated report endpoint (Phase D) — there's no
// rate-limiting infra in front of this route yet, so bounding payload size
// per-field is the only defense against a runaway/malicious client filling
// the table. Revisit once rate limiting exists.
const MAX_MESSAGE = 2000;
const MAX_STACK = 8000;
const MAX_CONTEXT_JSON = 4000;
const MAX_PATH = 500;
const MAX_SERVICE = 100;
const MAX_USER_ID = 200;

export interface ReportErrorInput {
  service: string;
  level?: ErrorEventLevel;
  message: string;
  stack?: string;
  context?: unknown;
  request_path?: string;
  user_id?: string;
}

export async function reportError(input: ReportErrorInput) {
  let contextJson = "{}";
  if (input.context !== undefined) {
    try {
      contextJson = JSON.stringify(input.context).slice(0, MAX_CONTEXT_JSON);
    } catch {
      contextJson = "{}";
    }
  }

  await db
    .insertInto("error_events")
    .values({
      service: input.service.slice(0, MAX_SERVICE),
      level: input.level === "warning" ? "warning" : "error",
      message: input.message.slice(0, MAX_MESSAGE),
      stack: input.stack ? input.stack.slice(0, MAX_STACK) : null,
      context: contextJson,
      request_path: input.request_path ? input.request_path.slice(0, MAX_PATH) : null,
      status_code: null,
      user_id: input.user_id ? input.user_id.slice(0, MAX_USER_ID) : null,
    })
    .execute();
}
