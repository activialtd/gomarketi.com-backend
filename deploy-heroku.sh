#!/usr/bin/env bash
# deploy-heroku.sh — build and deploy GoMarket services to Heroku
#
# Usage:
#   ./deploy-heroku.sh              # deploy all services
#   ./deploy-heroku.sh auth         # deploy one service
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

SERVICES=(auth identity storefront catalogue orders)
APP_NAMES=(
  "$APP_AUTH"
  "$APP_IDENTITY"
  "$APP_STOREFRONT"
  "$APP_CATALOGUE"
  "$APP_ORDERS"
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

  # Set config vars
  echo "Setting config vars..."
  set_vars=(
    "DATABASE_URL=$DATABASE_URL"
    "ENV=$ENV"
    "ALLOWED_ORIGINS=$ALLOWED_ORIGINS"
  )

  if [[ "$svc" == "auth" ]]; then
    set_vars+=(
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
  else
    # All other services only need the public key to validate JWT headers
    set_vars+=(
      "JWT_PUBLIC_KEY_B64=$JWT_PUBLIC_KEY_B64"
    )
  fi

  heroku config:set --app "$app" "${set_vars[@]}"

  # Build and push Docker image using the full repo as context
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
