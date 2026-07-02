#!/usr/bin/env bash
# deploy-railway.sh — set env vars and deploy GoMarketi services to Railway
#
# Usage:
#   ./deploy-railway.sh                        # deploy all → production
#   ./deploy-railway.sh auth                   # deploy auth → production
#   ./deploy-railway.sh gateway staging        # deploy gateway → staging
#   ./deploy-railway.sh --env staging          # deploy all → staging
#
# Prerequisites:
#   1. railway CLI installed  (npm install -g @railway/cli)
#   2. railway login
#   3. Services created in Railway dashboard for both environments
#   4. .env.railway (production) and .env.railway.staging filled in

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"

# ── Parse arguments ───────────────────────────────────────────────────────────
FILTER=""
ENVIRONMENT="production"

for arg in "$@"; do
  case "$arg" in
    staging|production) ENVIRONMENT="$arg" ;;
    --env=*) ENVIRONMENT="${arg#--env=}" ;;
    --env) ;;  # handled by next arg — not supported in this simple parser
    *) FILTER="$arg" ;;
  esac
done

# ── Load env file ─────────────────────────────────────────────────────────────
if [[ "$ENVIRONMENT" == "staging" ]]; then
  ENV_FILE="$REPO_ROOT/.env.railway.staging"
else
  ENV_FILE="$REPO_ROOT/.env.railway"
fi

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: $ENV_FILE not found."
  exit 1
fi

source "$ENV_FILE"

echo "Deploying to: $ENVIRONMENT"

SERVICES=(auth identity storefront catalogue orders gateway)
SVC_NAMES=(
  "$SVC_AUTH"
  "$SVC_IDENTITY"
  "$SVC_STOREFRONT"
  "$SVC_CATALOGUE"
  "$SVC_ORDERS"
  "$SVC_GATEWAY"
)

set_vars() {
  local svc="$1"
  local name="$2"
  local vars=(
    "ENV=$ENVIRONMENT"
    "ALLOWED_ORIGINS=$ALLOWED_ORIGINS"
  )

  case "$svc" in
    auth)
      vars+=(
        "DATABASE_URL=$DATABASE_URL"
        "JWT_PRIVATE_KEY_B64=$JWT_PRIVATE_KEY_B64"
        "JWT_PUBLIC_KEY_B64=$JWT_PUBLIC_KEY_B64"
        "BREVO_API_KEY=${BREVO_API_KEY:-}"
        "BREVO_FROM=${BREVO_FROM:-}"
        "BREVO_FROM_NAME=${BREVO_FROM_NAME:-GoMarketi}"
        "SMTP_HOST=${SMTP_HOST:-}"
        "SMTP_PORT=${SMTP_PORT:-}"
        "SMTP_USERNAME=${SMTP_USERNAME:-}"
        "SMTP_PASSWORD=${SMTP_PASSWORD:-}"
        "SMTP_FROM=${SMTP_FROM:-}"
        "GOOGLE_CLIENT_ID=$GOOGLE_CLIENT_ID"
        "APPLE_BUNDLE_ID=$APPLE_BUNDLE_ID"
      )
      ;;
    gateway)
      # Use Railway private networking — avoids public internet hops and loop detection
      vars+=(
        "UPSTREAM_AUTH=http://$SVC_AUTH.railway.internal:8080"
        "UPSTREAM_IDENTITY=http://$SVC_IDENTITY.railway.internal:8080"
        "UPSTREAM_STOREFRONT=http://$SVC_STOREFRONT.railway.internal:8080"
        "UPSTREAM_CATALOGUE=http://$SVC_CATALOGUE.railway.internal:8080"
        "UPSTREAM_ORDERS=http://$SVC_ORDERS.railway.internal:8080"
      )
      ;;
    storefront)
      vars+=(
        "DATABASE_URL=$DATABASE_URL"
        "JWT_PUBLIC_KEY_B64=$JWT_PUBLIC_KEY_B64"
        "BREVO_API_KEY=${BREVO_API_KEY:-}"
        "BREVO_FROM=${BREVO_FROM:-}"
        "BREVO_FROM_NAME=${BREVO_FROM_NAME:-GoMarketi}"
        "STORE_DOMAIN=${STORE_DOMAIN:-gomarketi.com}"
      )
      ;;
    *)
      vars+=(
        "DATABASE_URL=$DATABASE_URL"
        "JWT_PUBLIC_KEY_B64=$JWT_PUBLIC_KEY_B64"
      )
      ;;
  esac

  # Filter empty values
  local filtered=()
  for v in "${vars[@]}"; do
    local val="${v#*=}"
    [[ -n "$val" ]] && filtered+=("$v")
  done

  echo "  Setting ${#filtered[@]} env vars..."
  railway variables set "${filtered[@]}" --service "$name" --environment "$ENVIRONMENT" || {
    echo "  ⚠ railway variables set exited non-zero (vars may still have been applied — continuing)"
  }
}

deploy_service() {
  local svc="$1"
  local name="$2"

  echo ""
  echo "══════════════════════════════════════════"
  echo "  [$ENVIRONMENT] $svc → $name"
  echo "══════════════════════════════════════════"

  set_vars "$svc" "$name"

  echo "  Deploying..."
  railway up --service "$name" --environment "$ENVIRONMENT" --detach

  echo "✓ $svc queued"
}

for i in "${!SERVICES[@]}"; do
  svc="${SERVICES[$i]}"
  name="${SVC_NAMES[$i]}"

  if [[ -n "$FILTER" && "$svc" != "$FILTER" && "$FILTER" != "staging" && "$FILTER" != "production" ]]; then
    continue
  fi

  deploy_service "$svc" "$name"
done

echo ""
echo "All done → $ENVIRONMENT"
