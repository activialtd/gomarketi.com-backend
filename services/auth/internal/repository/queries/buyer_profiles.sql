-- name: CreateBuyerProfile :one
INSERT INTO buyer_profiles (user_id)
VALUES ($1)
RETURNING id, user_id, total_orders, total_spent, created_at, updated_at;

-- name: GetBuyerProfileByUserID :one
SELECT id, user_id, total_orders, total_spent, created_at, updated_at
FROM buyer_profiles
WHERE user_id = $1;
