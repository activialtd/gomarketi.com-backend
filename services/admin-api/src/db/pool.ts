import pg from "pg";
import { Kysely, PostgresDialect } from "kysely";
import { config } from "../config.js";
import type { Database } from "./types.js";

export const pool = new pg.Pool({ connectionString: config.databaseUrl, max: 10 });

export const db = new Kysely<Database>({
  dialect: new PostgresDialect({ pool }),
});
