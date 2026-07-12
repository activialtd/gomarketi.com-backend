#!/usr/bin/env bash
# Redeploy a single service with a new image tag, without touching the
# other 7 containers. Invoked by CI via SSM Run Command after pushing a new
# image: ./deploy-service.sh <service> <tag>
set -euo pipefail

SERVICE="${1:?usage: deploy-service.sh <service> <tag>}"
TAG="${2:?usage: deploy-service.sh <service> <tag>}"

case "$SERVICE" in
  auth|identity|storefront|catalogue|orders|gateway) ;;
  *) echo "unknown service: $SERVICE" >&2; exit 1 ;;
esac

VAR_NAME="IMAGE_TAG_$(echo "$SERVICE" | tr '[:lower:]' '[:upper:]')"

cd "$(dirname "$0")"

if grep -q "^${VAR_NAME}=" .env; then
  sed -i "s|^${VAR_NAME}=.*|${VAR_NAME}=${TAG}|" .env
else
  echo "${VAR_NAME}=${TAG}" >> .env
fi

set -a
source .env
set +a

aws ecr get-login-password --region "$AWS_REGION" | docker login --username AWS --password-stdin "$ECR_REGISTRY"
docker compose -f docker-compose.prod.yml pull "$SERVICE"
docker compose -f docker-compose.prod.yml up -d --no-deps "$SERVICE"

echo "deployed ${SERVICE}:${TAG}"
