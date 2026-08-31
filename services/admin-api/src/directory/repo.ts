import { sql } from "kysely";
import { db } from "../db/pool.js";

const DEFAULT_PER_PAGE = 20;

function clampPaging(page?: number, perPage?: number) {
  const p = Math.max(1, page ?? 1);
  const pp = Math.min(100, Math.max(1, perPage ?? DEFAULT_PER_PAGE));
  return { page: p, perPage: pp, offset: (p - 1) * pp };
}

// ── Customers ──────────────────────────────────────────────────────────────

export async function listCustomers(opts: { q?: string; page?: number; perPage?: number }) {
  const { page, perPage, offset } = clampPaging(opts.page, opts.perPage);

  let query = db
    .selectFrom("users")
    .innerJoin("buyer_profiles", "buyer_profiles.user_id", "users.id")
    .select([
      "users.id",
      "users.email",
      "users.full_name",
      "users.phone",
      "users.is_active",
      "users.created_at",
      "buyer_profiles.total_orders",
      "buyer_profiles.total_spent",
    ]);

  if (opts.q) {
    const like = `%${opts.q}%`;
    query = query.where((eb) =>
      eb.or([eb("users.email", "ilike", like), eb("users.full_name", "ilike", like)]),
    );
  }

  const [rows, total] = await Promise.all([
    query.orderBy("users.created_at", "desc").limit(perPage).offset(offset).execute(),
    countCustomers(opts.q),
  ]);

  return { customers: rows, total, page, per_page: perPage };
}

export async function getCustomer(userId: string) {
  const profile = await db
    .selectFrom("users")
    .innerJoin("buyer_profiles", "buyer_profiles.user_id", "users.id")
    .select([
      "users.id",
      "users.email",
      "users.full_name",
      "users.phone",
      "users.avatar_url",
      "users.is_active",
      "users.created_at",
      "buyer_profiles.total_orders",
      "buyer_profiles.total_spent",
    ])
    .where("users.id", "=", userId)
    .executeTakeFirst();

  if (!profile) return null;

  const orders = await db
    .selectFrom("orders")
    .leftJoin("order_items", "order_items.order_id", "orders.id")
    .select([
      "orders.id",
      "orders.store_id",
      "orders.status",
      "orders.total_kobo",
      "orders.created_at",
      sql<
        { name: string; quantity: number; price_kobo: string; image_url: string }[]
      >`coalesce(json_agg(json_build_object(
          'name', order_items.name,
          'quantity', order_items.quantity,
          'price_kobo', order_items.price_kobo,
          'image_url', order_items.image_url
        )) filter (where order_items.id is not null), '[]')`.as("items"),
    ])
    .where("orders.customer_id", "=", userId)
    .groupBy(["orders.id"])
    .orderBy("orders.created_at", "desc")
    .limit(50)
    .execute();

  return { profile, orders };
}

// ── Vendors ────────────────────────────────────────────────────────────────

export async function listVendors(opts: { q?: string; page?: number; perPage?: number }) {
  const { page, perPage, offset } = clampPaging(opts.page, opts.perPage);

  let query = db
    .selectFrom("users")
    .innerJoin("vendor_profiles", "vendor_profiles.user_id", "users.id")
    .select([
      "users.id",
      "users.email",
      "users.full_name",
      "users.phone",
      "users.created_at",
      "vendor_profiles.business_name",
      "vendor_profiles.kyc_status",
      "vendor_profiles.onboarding_step",
      "vendor_profiles.is_active",
    ]);

  if (opts.q) {
    const like = `%${opts.q}%`;
    query = query.where((eb) =>
      eb.or([
        eb("users.email", "ilike", like),
        eb("users.full_name", "ilike", like),
        eb("vendor_profiles.business_name", "ilike", like),
      ]),
    );
  }

  const [rows, total] = await Promise.all([
    query.orderBy("users.created_at", "desc").limit(perPage).offset(offset).execute(),
    countVendors(opts.q),
  ]);

  return { vendors: rows, total, page, per_page: perPage };
}

export async function getVendor(userId: string) {
  const profile = await db
    .selectFrom("users")
    .innerJoin("vendor_profiles", "vendor_profiles.user_id", "users.id")
    .leftJoin("vendor_subscriptions", "vendor_subscriptions.vendor_profile_id", "vendor_profiles.id")
    .leftJoin("plans", "plans.id", "vendor_subscriptions.plan_id")
    .select([
      "users.id",
      "users.email",
      "users.full_name",
      "users.phone",
      "users.created_at",
      "vendor_profiles.id as vendor_profile_id",
      "vendor_profiles.business_name",
      "vendor_profiles.business_type",
      "vendor_profiles.kyc_status",
      "vendor_profiles.onboarding_step",
      "vendor_profiles.is_active",
      "vendor_profiles.paystack_dva_account_number",
      "vendor_profiles.paystack_dva_bank_name",
      "vendor_subscriptions.status as subscription_status",
      "vendor_subscriptions.current_period_end",
      "plans.slug as plan_slug",
      "plans.display_name as plan_name",
    ])
    .where("users.id", "=", userId)
    .executeTakeFirst();

  if (!profile) return null;

  const stores = await db
    .selectFrom("stores")
    .select(["id", "name", "slug", "category", "currency", "is_active", "created_at"])
    .where("vendor_id", "=", userId)
    .orderBy("created_at", "desc")
    .execute();

  const sales =
    stores.length === 0
      ? []
      : await db
          .selectFrom("orders")
          .innerJoin("stores", "stores.id", "orders.store_id")
          .select([
            "orders.id",
            "orders.store_id",
            "stores.name as store_name",
            "orders.customer_name",
            "orders.status",
            "orders.total_kobo",
            "orders.created_at",
          ])
          .where("stores.vendor_id", "=", userId)
          .orderBy("orders.created_at", "desc")
          .limit(50)
          .execute();

  return { profile, stores, sales };
}

// ── Helpers ────────────────────────────────────────────────────────────────

async function countCustomers(q?: string): Promise<number> {
  let query = db
    .selectFrom("users")
    .innerJoin("buyer_profiles", "buyer_profiles.user_id", "users.id")
    .select(sql<string>`count(*)`.as("count"));

  if (q) {
    const like = `%${q}%`;
    query = query.where((eb) => eb.or([eb("users.email", "ilike", like), eb("users.full_name", "ilike", like)]));
  }

  const result = await query.executeTakeFirst();
  return Number(result?.count ?? 0);
}

async function countVendors(q?: string): Promise<number> {
  let query = db
    .selectFrom("users")
    .innerJoin("vendor_profiles", "vendor_profiles.user_id", "users.id")
    .select(sql<string>`count(*)`.as("count"));

  if (q) {
    const like = `%${q}%`;
    query = query.where((eb) =>
      eb.or([
        eb("users.email", "ilike", like),
        eb("users.full_name", "ilike", like),
        eb("vendor_profiles.business_name", "ilike", like),
      ]),
    );
  }

  const result = await query.executeTakeFirst();
  return Number(result?.count ?? 0);
}
