// Runs all pending SQL migrations from db/migrations/ against DATABASE_URL.
// Mirrors scripts/migrate/main.go's shape exactly, one level down: numbered
// .sql files applied in order inside a transaction, tracked in a table.
//
// Deliberately named admin_schema_migrations, not schema_migrations — that
// name is already used by the Go migration runner against this same
// physical database; a distinct name avoids any confusion between two
// independently-versioned migration histories.
//
// Usage: npm run migrate (from services/admin-api)
import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import pg from "pg";
import { config } from "../src/config.js";

const migrationsDir = path.join(import.meta.dirname, "..", "db", "migrations");

const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS admin_schema_migrations (
    version    TEXT        PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);`;

async function main() {
  const pool = new pg.Pool({ connectionString: config.databaseUrl, max: 3 });

  await pingWithRetry(pool);
  console.log("connected to database");

  await pool.query(createMigrationsTable);

  const files = readdirSync(migrationsDir)
    .filter((f) => f.endsWith(".sql"))
    .sort();

  if (files.length === 0) {
    console.log(`no migration files found in ${migrationsDir}`);
    await pool.end();
    return;
  }

  let applied = 0;
  for (const file of files) {
    const version = file.replace(/\.sql$/, "");
    const { rows } = await pool.query(
      `SELECT EXISTS(SELECT 1 FROM admin_schema_migrations WHERE version = $1) AS exists`,
      [version],
    );
    if (rows[0].exists) {
      console.log(`  skip  ${version}`);
      continue;
    }

    const content = readFileSync(path.join(migrationsDir, file), "utf8");
    const client = await pool.connect();
    try {
      await client.query("BEGIN");
      await client.query(content);
      await client.query(`INSERT INTO admin_schema_migrations (version) VALUES ($1)`, [version]);
      await client.query("COMMIT");
      console.log(`  apply ${version} ✓`);
      applied++;
    } catch (err) {
      await client.query("ROLLBACK");
      throw new Error(`applying ${version}: ${err instanceof Error ? err.message : err}`);
    } finally {
      client.release();
    }
  }

  console.log(`done — ${applied} migration(s) applied`);
  await pool.end();
}

async function pingWithRetry(pool: pg.Pool, maxAttempts = 5, delayMs = 2000): Promise<void> {
  let lastErr: unknown;
  for (let i = 1; i <= maxAttempts; i++) {
    try {
      await pool.query("SELECT 1");
      return;
    } catch (err) {
      lastErr = err;
      console.log(`  ping ${i}/${maxAttempts} failed: ${err instanceof Error ? err.message : err}`);
      if (i < maxAttempts) await new Promise((r) => setTimeout(r, delayMs));
    }
  }
  throw new Error(`after ${maxAttempts} attempts: ${lastErr instanceof Error ? lastErr.message : lastErr}`);
}

main().catch((err) => {
  console.error(err instanceof Error ? err.message : err);
  process.exit(1);
});
