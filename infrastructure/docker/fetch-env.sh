#!/usr/bin/env bash
# Pulls this environment's secrets out of SSM Parameter Store
# (/gomarketi/<env>/<service>/<KEY>) into per-service env files that
# docker-compose.prod.yml loads via `env_file:`. Run before every
# `docker compose up` (first boot and every deploy) so rotated secrets take
# effect on the next container start.
set -euo pipefail

: "${GOMARKETI_ENV:?set GOMARKETI_ENV to staging or production}"
: "${AWS_REGION:?set AWS_REGION}"

OUT_DIR="$(dirname "$0")/env"
mkdir -p "$OUT_DIR"

for svc in auth identity storefront catalogue orders gateway; do
  aws ssm get-parameters-by-path \
    --path "/gomarketi/${GOMARKETI_ENV}/${svc}" \
    --with-decryption \
    --region "${AWS_REGION}" \
    --query 'Parameters[].{Name:Name,Value:Value}' \
    --output json \
  | python3 -c '
import json, sys
for p in json.load(sys.stdin):
    key = p["Name"].rsplit("/", 1)[-1]
    val = p["Value"].replace("\n", "\\n")
    print(f"{key}={val}")
' > "${OUT_DIR}/${svc}.env"
done

echo "Wrote env files for: auth identity storefront catalogue orders gateway"
