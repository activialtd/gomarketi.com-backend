-- 0009_add_password_hash.sql
-- Adds optional password-based authentication alongside OTP and OAuth.
-- password_hash is NULL for users who registered via Google or Apple —
-- attempting to login with a password when this column is NULL returns a
-- "use your social provider" error, not a generic auth failure.
-- bcrypt cost is handled at the application layer; the column stores the
-- full bcrypt string (e.g. "$2a$12$...").

ALTER TABLE users ADD COLUMN password_hash TEXT;
