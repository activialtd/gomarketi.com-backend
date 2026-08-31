-- 0002_create_error_events.sql
-- Unified error queue for the Admin Center's error-tracking screen. Rows land
-- here two ways: Go services write directly via shared/pkg/middleware.Recovery
-- on panic/5xx (service in {auth,identity,storefront,catalogue,orders}), and
-- frontend apps POST to admin-api's /v1/admin/errors/report (service in
-- {vendor-web,consumer-app}) for self-built crash capture. One table, one
-- queue, regardless of origin.

CREATE TABLE IF NOT EXISTS error_events (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    service       TEXT        NOT NULL,
    level         TEXT        NOT NULL DEFAULT 'error'
                              CHECK (level IN ('error', 'warning')),
    message       TEXT        NOT NULL,
    stack         TEXT,
    context       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    request_path  TEXT,
    status_code   INTEGER,
    user_id       TEXT,
    resolved      BOOLEAN     NOT NULL DEFAULT FALSE,
    resolved_at   TIMESTAMPTZ,
    resolved_by   UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_error_events_unresolved
    ON error_events (created_at DESC)
    WHERE resolved = FALSE;

CREATE INDEX IF NOT EXISTS idx_error_events_service
    ON error_events (service, created_at DESC);
