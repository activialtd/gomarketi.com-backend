#!/usr/bin/env bash
# deploy-railway.sh — set env vars and deploy GoMarketi services to Railway
#
# Usage:
#   ./deploy-railway.sh              # deploy all services
#   ./deploy-railway.sh auth         # deploy one service
#   ./deploy-railway.sh gateway      # deploy only gateway
#
# Prerequisites:
#   1. railway CLI installed  (npm install -g @railway/cli)
#   2. railway login
#   3. Services already created in Railway dashboard (one-time — see README)
#   4. .env.railway filled in (copy from .env.railway.template)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
ENV_FILE="$REPO_ROOT/.env.railway"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: $ENV_FILE not found."
  echo "Copy .env.railway.template to .env.railway and fill in the values."
  exit 1
fi

source "$ENV_FILE"

SERVICES=(auth identity storefront catalogue orders gateway)
SVC_NAMES=(
  "$SVC_AUTH"
  "$SVC_IDENTITY"
  "$SVC_STOREFRONT"
  "$SVC_CATALOGUE"
  "$SVC_ORDERS"
  "$SVC_GATEWAY"
)

FILTER="${1:-}"

set_vars() {
  local svc="$1"
  local name="$2"
  local vars=()

  vars+=(
    "ENV=production"
    "ALLOWED_ORIGINS=$ALLOWED_ORIGINS"
  )

  case "$svc" in
    auth)
      vars+=(
        "DATABASE_URL=$DATABASE_URL"
        "JWT_PRIVATE_KEY_B64=$JWT_PRIVATE_KEY_B64"
        "JWT_PUBLIC_KEY_B64=$JWT_PUBLIC_KEY_B64"
        "SMTP_HOST=$SMTP_HOST"
        "SMTP_PORT=$SMTP_PORT"
        "SMTP_USERNAME=$SMTP_USERNAME"
        "SMTP_PASSWORD=$SMTP_PASSWORD"
        "SMTP_FROM=$SMTP_FROM"
        "GOOGLE_CLIENT_ID=$GOOGLE_CLIENT_ID"
        "APPLE_BUNDLE_ID=$APPLE_BUNDLE_ID"
      )
      ;;
    gateway)
      vars+=(
        "UPSTREAM_AUTH=https://$SVC_AUTH.up.railway.app"
        "UPSTREAM_IDENTITY=https://$SVC_IDENTITY.up.railway.app"
        "UPSTREAM_STOREFRONT=https://$SVC_STOREFRONT.up.railway.app"
        "UPSTREAM_CATALOGUE=https://$SVC_CATALOGUE.up.railway.app"
        "UPSTREAM_ORDERS=https://$SVC_ORDERS.up.railway.app"
      )
      ;;
    *)
      vars+=(
        "DATABASE_URL=$DATABASE_URL"
        "JWT_PUBLIC_KEY_B64=$JWT_PUBLIC_KEY_B64"
      )
      ;;
  esac

  # Filter out empty values — Railway rejects KEY= with no value
  local filtered=()
  for v in "${vars[@]}"; do
    local val="${v#*=}"
    [[ -n "$val" ]] && filtered+=("$v")
  done

  echo "  Setting ${#filtered[@]} env vars..."
  railway variables set "${filtered[@]}" --service "$name"
}

deploy_service() {
  local svc="$1"
  local name="$2"

  echo ""
  echo "══════════════════════════════════════════"
  echo "  Deploying: $svc → $name"
  echo "══════════════════════════════════════════"

  set_vars "$svc" "$name"

  echo "  Deploying..."
  railway up --service "$name" --detach

  echo "✓ $svc queued → https://$name.up.railway.app"
}

for i in "${!SERVICES[@]}"; do
  svc="${SERVICES[$i]}"
  name="${SVC_NAMES[$i]}"

  if [[ -n "$FILTER" && "$svc" != "$FILTER" ]]; then
    continue
  fi

  deploy_service "$svc" "$name"
done

echo ""
echo "All services queued. Watch progress at https://railway.app"
echo ""
echo "Next: add api.activialtd.com as a custom domain on the gateway service:"
echo "  railway domain --service $SVC_GATEWAY"
