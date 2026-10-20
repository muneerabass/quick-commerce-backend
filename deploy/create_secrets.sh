#!/usr/bin/env bash
# Run once to populate all Secret Manager secrets.
# Usage: DB_PASSWORD=<password> bash deploy/create_secrets.sh [CLOUD_RUN_URL]
#
# DB_PASSWORD is the password you chose when running bootstrap.sh or the
# gcloud sql users create command. It is NEVER stored in this file or in git.

set -euo pipefail

PROJECT=project-5dababb2-6d66-4348-a84
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$SCRIPT_DIR/.env"

if [[ -z "${DB_PASSWORD:-}" ]]; then
  echo "ERROR: set DB_PASSWORD before running this script."
  echo "  DB_PASSWORD=<your-db-password> bash deploy/create_secrets.sh"
  exit 1
fi

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: $ENV_FILE not found"; exit 1
fi

env_val() {
  grep -E "^${1}=" "$ENV_FILE" | head -1 | cut -d= -f2-
}

store() {
  local name=$1 value=$2
  if [[ -z "$value" ]]; then
    echo "  SKIP $name (empty)"
    return
  fi
  printf '%s' "$value" | gcloud secrets create "$name" \
    --data-file=- --project="$PROJECT" 2>/dev/null \
  || printf '%s' "$value" | gcloud secrets versions add "$name" \
    --data-file=- --project="$PROJECT"
  echo "  OK   $name"
}

echo "==> Creating secrets in $PROJECT"

DB_HOST="/cloudsql/project-5dababb2-6d66-4348-a84:asia-south1:quick-commerce-pg"
store DATABASE_URL         "postgresql://quickcommerce:${DB_PASSWORD}@localhost/quickcommerce?host=${DB_HOST}"
store JWT_SECRET           "$(openssl rand -base64 32)"
store OTP_HASH_SECRET      "$(openssl rand -base64 32)"
store DELIVERY_CODE_SECRET "$(openssl rand -base64 32)"

store TWO_FACTOR_API_KEY        "$(env_val TWO_FACTOR_API_KEY)"
store GOOGLE_MAPS_PLATFORM_KEY  "$(env_val GOOGLE_MAPS_PLATFORM_KEY)"
store CLOUDINARY_CLOUD_NAME     "$(env_val CLOUDINARY_CLOUD_NAME)"
store CLOUDINARY_API_KEY        "$(env_val CLOUDINARY_API_KEY)"
store CLOUDINARY_API_SECRET     "$(env_val CLOUDINARY_API_SECRET)"

CLOUD_RUN_URL="${1:-}"
if [[ -n "$CLOUD_RUN_URL" ]]; then
  store ALLOWED_ORIGINS "$CLOUD_RUN_URL"
else
  echo "  NOTE ALLOWED_ORIGINS: run again after first deploy:"
  echo "       DB_PASSWORD=\$DB_PASSWORD bash deploy/create_secrets.sh https://quick-commerce-api-xxxx.run.app"
fi

echo ""
echo "==> Done. Verify: gcloud secrets list --project=$PROJECT"
