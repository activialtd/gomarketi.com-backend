#!/usr/bin/env bash
# deploy-heroku.sh — build and deploy GoMarket services to Heroku
#
# Usage:
#   ./deploy-heroku.sh              # deploy all services + gateway
#   ./deploy-heroku.sh auth         # deploy one service
#   ./deploy-heroku.sh gateway      # deploy only the gateway
#
# Prerequisites:
#   1. heroku CLI installed and logged in (heroku login)
#   2. .env.heroku filled in from .env.heroku.template
#   3. Heroku apps already created (script will create them if missing)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
ENV_FILE="$REPO_ROOT/.env.heroku"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: $ENV_FILE not found."
  echo "Copy .env.heroku.template to .env.heroku and fill in the values."
  exit 1
fi

# shellcheck source=/dev/null
source "$ENV_FILE"

SERVICES=(auth identity storefront catalogue orders gateway)
APP_NAMES=(
  "$APP_AUTH"
  "$APP_IDENTITY"
  "$APP_STOREFRONT"
  "$APP_CATALOGUE"
  "$APP_ORDERS"
  "$APP_GATEWAY"
)

# If a service name was passed as argument, deploy only that one
FILTER="${1:-}"

deploy_service() {
  local svc="$1"
  local app="$2"

  echo ""
  echo "══════════════════════════════════════════"
  echo "  Deploying: $svc → $app"
  echo "══════════════════════════════════════════"

  # Create app if it doesn't exist
  if ! heroku apps:info --app "$app" &>/dev/null; then
    echo "Creating Heroku app: $app"
    heroku apps:create "$app"
  fi

  # Build config vars for this service
  echo "Setting config vars..."
  set_vars=("ENV=$ENV" "ALLOWED_ORIGINS=$ALLOWED_ORIGINS")

  case "$svc" in
    auth)
      set_vars+=(
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
      # Gateway needs the upstream app URLs — no DB access, no JWT keys
      set_vars+=(
        "UPSTREAM_AUTH=https://$APP_AUTH.herokuapp.com"
        "UPSTREAM_IDENTITY=https://$APP_IDENTITY.herokuapp.com"
        "UPSTREAM_STOREFRONT=https://$APP_STOREFRONT.herokuapp.com"
        "UPSTREAM_CATALOGUE=https://$APP_CATALOGUE.herokuapp.com"
        "UPSTREAM_ORDERS=https://$APP_ORDERS.herokuapp.com"
      )
      ;;
    *)
      # All backend services share the DB and need the JWT public key
      set_vars+=(
        "DATABASE_URL=$DATABASE_URL"
        "JWT_PUBLIC_KEY_B64=$JWT_PUBLIC_KEY_B64"
      )
      ;;
  esac

  heroku config:set --app "$app" "${set_vars[@]}"

  # Build and push Docker image — full repo root is always the build context
  echo "Building and pushing Docker image..."
  heroku container:push web \
    --app "$app" \
    --dockerfile "services/$svc/Dockerfile"

  # Release
  echo "Releasing..."
  heroku container:release web --app "$app"

  echo "✓ $svc deployed → https://$app.herokuapp.com"
}

for i in "${!SERVICES[@]}"; do
  svc="${SERVICES[$i]}"
  app="${APP_NAMES[$i]}"

  if [[ -n "$FILTER" && "$svc" != "$FILTER" ]]; then
    continue
  fi

  deploy_service "$svc" "$app"
done

echo ""
echo "All done."
echo ""
echo "Next: add api.gomarketi.com to the gateway app:"
echo "  heroku domains:add api.gomarketi.com --app \$APP_GATEWAY"
echo "  heroku domains --app \$APP_GATEWAY   # shows the DNS target CNAME value"
