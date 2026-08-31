// Hand-written Kysely schema types, added to incrementally as each phase's
// queries need them — covers only the tables admin-api actually reads today.
// Not full kysely-codegen output against the whole shared database (that
// would pull in every other service's tables for no benefit); this is the
// minimal typed surface this service touches, kept in sync by hand.
import type { Generated } from "kysely";

export interface AdminUsersTable {
  id: Generated<string>;
  email: string;
  full_name: string;
  password_hash: string;
  role: "agent" | "supervisor" | "super_admin";
  is_active: Generated<boolean>;
  last_login_at: Date | null;
  created_at: Generated<Date>;
  updated_at: Generated<Date>;
}

// ── Read-only tables owned by other services (identity/storefront/orders) ──
// admin-api never writes to these — see services/admin-api/src/directory.
// Columns are the subset this service actually selects, not necessarily
// the table's full column list.

export interface UsersTable {
  id: string;
  email: string | null;
  full_name: string | null;
  avatar_url: string | null;
  phone: string | null;
  is_email_verified: boolean;
  is_active: boolean;
  created_at: Date;
  updated_at: Date;
}

export interface BuyerProfilesTable {
  id: string;
  user_id: string;
  total_orders: number;
  total_spent: string; // bigint (kobo) comes back as string from pg by default
  created_at: Date;
}

export interface VendorProfilesTable {
  id: string;
  user_id: string;
  business_name: string | null;
  business_type: string | null;
  kyc_status: string;
  onboarding_step: string;
  is_active: boolean;
  paystack_dva_account_number: string | null;
  paystack_dva_bank_name: string | null;
  created_at: Date;
}

export interface VendorSubscriptionsTable {
  id: string;
  vendor_profile_id: string;
  plan_id: string;
  status: string;
  current_period_start: Date;
  current_period_end: Date | null;
}

export interface PlansTable {
  id: string;
  slug: string;
  display_name: string;
  price_kobo: string;
}

export interface StoresTable {
  id: string;
  vendor_id: string;
  name: string;
  slug: string;
  category: string;
  currency: string;
  logo_url: string | null;
  is_active: boolean;
  created_at: Date;
}

export type OrderStatus = "pending" | "confirmed" | "at_hub" | "shipped" | "delivered" | "cancelled";

export interface OrdersTable {
  id: string;
  store_id: string;
  customer_id: string;
  customer_name: string;
  customer_email: string;
  status: OrderStatus;
  total_kobo: string;
  delivery_address: string;
  payment_reference: string | null;
  hub_received_at: Date | null;
  hub_received_by: string | null;
  dispatched_at: Date | null;
  delivered_at: Date | null;
  delivery_confirmed_at: Date | null;
  cancelled_reason: string | null;
  refund_reference: string | null;
  refunded_at: Date | null;
  created_at: Date;
  updated_at: Date;
}

export interface OrderItemsTable {
  id: string;
  order_id: string;
  product_id: string;
  name: string;
  image_url: string;
  quantity: number;
  price_kobo: string;
}

export interface WalletTransactionsTable {
  id: string;
  store_id: string;
  type: "credit" | "debit";
  amount_kobo: string;
  order_id: string | null;
  status: "pending" | "completed" | "failed";
  released_at: Date | null;
  created_at: Date;
}

export interface Database {
  admin_users: AdminUsersTable;
  users: UsersTable;
  buyer_profiles: BuyerProfilesTable;
  vendor_profiles: VendorProfilesTable;
  vendor_subscriptions: VendorSubscriptionsTable;
  plans: PlansTable;
  stores: StoresTable;
  orders: OrdersTable;
  order_items: OrderItemsTable;
  wallet_transactions: WalletTransactionsTable;
}
