import { sql } from "kysely";
import { db } from "../db/pool.js";
import { config } from "../config.js";

const DEFAULT_PER_PAGE = 20;

function clampPaging(page?: number, perPage?: number) {
  const p = Math.max(1, page ?? 1);
  const pp = Math.min(100, Math.max(1, perPage ?? DEFAULT_PER_PAGE));
  return { page: p, perPage: pp, offset: (p - 1) * pp };
}

// A "batch" is every order sharing one payment_reference (the user's
// multi-vendor cart checkout) — no dedicated table, matches the codebase's
// own existing findOrdersByPaymentRef precedent in the Go orders service.

export interface BatchSummary {
  payment_reference: string;
  customer_name: string;
  customer_email: string;
  order_count: number;
  at_hub_count: number;
  shipped_count: number;
  delivered_count: number;
  cancelled_count: number;
  total_kobo: string;
  created_at: Date;
}

export async function listBatches(opts: { page?: number; perPage?: number }) {
  const { page, perPage, offset } = clampPaging(opts.page, opts.perPage);

  const rows = await sql<BatchSummary>`
    SELECT payment_reference, customer_name, customer_email,
           count(*)::int AS order_count,
           count(*) FILTER (WHERE hub_received_at IS NOT NULL)::int AS at_hub_count,
           count(*) FILTER (WHERE status = 'shipped')::int AS shipped_count,
           count(*) FILTER (WHERE status = 'delivered')::int AS delivered_count,
           count(*) FILTER (WHERE status = 'cancelled')::int AS cancelled_count,
           sum(total_kobo) AS total_kobo,
           min(created_at) AS created_at
    FROM orders
    WHERE payment_reference IS NOT NULL
    GROUP BY payment_reference, customer_name, customer_email
    ORDER BY min(created_at) DESC
    LIMIT ${perPage} OFFSET ${offset}
  `.execute(db);

  const totalRow = await sql<{ count: string }>`
    SELECT count(DISTINCT payment_reference) AS count FROM orders WHERE payment_reference IS NOT NULL
  `.execute(db);

  return {
    batches: rows.rows,
    total: Number(totalRow.rows[0]?.count ?? 0),
    page,
    per_page: perPage,
  };
}

export async function getBatch(paymentReference: string) {
  const orders = await db
    .selectFrom("orders")
    .innerJoin("stores", "stores.id", "orders.store_id")
    .leftJoin("wallet_transactions", (join) =>
      join.onRef("wallet_transactions.order_id", "=", "orders.id").on("wallet_transactions.type", "=", "credit"),
    )
    .select([
      "orders.id",
      "orders.store_id",
      "stores.name as store_name",
      "orders.customer_name",
      "orders.customer_email",
      "orders.status",
      "orders.total_kobo",
      "orders.hub_received_at",
      "orders.dispatched_at",
      "orders.delivered_at",
      "orders.delivery_confirmed_at",
      "orders.cancelled_reason",
      "orders.refund_reference",
      "orders.created_at",
      "wallet_transactions.status as wallet_status",
    ])
    .where("orders.payment_reference", "=", paymentReference)
    .orderBy("orders.created_at", "asc")
    .execute();

  if (orders.length === 0) return null;

  const items = await db
    .selectFrom("order_items")
    .select(["order_id", "name", "quantity", "price_kobo", "image_url"])
    .where(
      "order_id",
      "in",
      orders.map((o) => o.id),
    )
    .execute();

  return {
    payment_reference: paymentReference,
    customer_name: orders[0]!.customer_name,
    customer_email: orders[0]!.customer_email,
    orders: orders.map((o) => ({
      ...o,
      items: items.filter((i) => i.order_id === o.id),
    })),
  };
}

export async function hubIntake(orderId: string, adminId: string) {
  const result = await db
    .updateTable("orders")
    .set({ status: "at_hub", hub_received_at: new Date(), hub_received_by: adminId, updated_at: new Date() })
    .where("id", "=", orderId)
    .where("status", "=", "confirmed")
    .executeTakeFirst();

  if (result.numUpdatedRows === 0n) {
    throw new Error("order not found, or not in a state that can be checked in (must be 'confirmed')");
  }
}

export interface DispatchResult {
  shipped: string[];
  refunded: string[];
  refund_errors: { order_id: string; error: string }[];
}

// dispatchBatch is the core ops action: every order that made it to the hub
// ships together; every order whose vendor didn't gets cancelled and
// refunded via the one internal call to orders service (the only step that
// needs orders' own Paystack secret key).
export async function dispatchBatch(paymentReference: string): Promise<DispatchResult> {
  const orders = await db
    .selectFrom("orders")
    .select(["id", "status", "hub_received_at"])
    .where("payment_reference", "=", paymentReference)
    .execute();

  if (orders.length === 0) {
    throw new Error("batch not found");
  }

  const result: DispatchResult = { shipped: [], refunded: [], refund_errors: [] };

  for (const order of orders) {
    if (order.status !== "confirmed" && order.status !== "at_hub") {
      continue; // already shipped/delivered/cancelled — dispatch is not re-processed
    }
    if (order.hub_received_at) {
      await db
        .updateTable("orders")
        .set({ status: "shipped", dispatched_at: new Date(), updated_at: new Date() })
        .where("id", "=", order.id)
        .execute();
      result.shipped.push(order.id);
    } else {
      try {
        await callNoShowRefund(order.id);
        result.refunded.push(order.id);
      } catch (err) {
        result.refund_errors.push({ order_id: order.id, error: err instanceof Error ? err.message : String(err) });
      }
    }
  }

  return result;
}

async function callNoShowRefund(orderId: string): Promise<void> {
  const res = await fetch(`${config.ordersInternalUrl}/v1/orders/internal/no-show-refund`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Internal-Key": config.internalApiKey },
    body: JSON.stringify({ order_id: orderId }),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`no-show refund failed (${res.status}): ${body}`);
  }
}

// releaseEscrow is the manual override for disputes — releases a held
// wallet credit without going through buyer confirmation. Deliberately
// narrow: it only ever flips the wallet row, never touches order status,
// since a dispute resolution isn't the same claim as "the buyer got it."
export async function releaseEscrow(orderId: string) {
  const result = await db
    .updateTable("wallet_transactions")
    .set({ status: "completed", released_at: new Date() })
    .where("order_id", "=", orderId)
    .where("status", "=", "pending")
    .executeTakeFirst();

  if (result.numUpdatedRows === 0n) {
    throw new Error("no held wallet credit found for this order");
  }
}
