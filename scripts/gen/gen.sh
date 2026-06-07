#!/usr/bin/env bash
# Run sqlc generate for every Go service that has a sqlc.yaml.
# Run from the repo root: bash scripts/gen/gen.sh  (or: make gen)
set -euo pipefail

SERVICES=(auth identity catalogue orders storefront)
GENERATED=0

for svc in "${SERVICES[@]}"; do
    YAML="services/$svc/sqlc.yaml"
    if [[ -f "$YAML" ]]; then
        echo "→ sqlc generate: services/$svc"
        (cd "services/$svc" && sqlc generate)
        GENERATED=$((GENERATED + 1))
    fi
done

if [[ $GENERATED -eq 0 ]]; then
    echo "no sqlc.yaml files found — nothing generated"
else
    echo "done — $GENERATED service(s) generated"
fi
