#!/usr/bin/env bash
# One-time GCP setup for quick-commerce Cloud Run deployment.
# Run this from your local machine ONCE after installing gcloud CLI.
# Usage: bash deploy/bootstrap.sh <GCP_PROJECT_ID> <ADMIN_PHONE>
#   e.g. bash deploy/bootstrap.sh my-gcp-project +919876543210

set -euo pipefail

PROJECT=${1:?'Usage: bootstrap.sh <GCP_PROJECT_ID> <ADMIN_PHONE>'}
ADMIN_PHONE=${2:?'Usage: bootstrap.sh <GCP_PROJECT_ID> <ADMIN_PHONE>'}
REGION=asia-south1
SERVICE=quick-commerce-api
REPO=quick-commerce
SA_NAME=quick-commerce-run
DB_INSTANCE=quick-commerce-pg
DB_NAME=quickcommerce
DB_USER=quickcommerce

echo "==> Setting project to $PROJECT"
gcloud config set project "$PROJECT"

echo "==> Enabling required APIs"
gcloud services enable \
  run.googleapis.com \
  sqladmin.googleapis.com \
  secretmanager.googleapis.com \
  artifactregistry.googleapis.com \
  cloudbuild.googleapis.com

echo "==> Creating Artifact Registry repository"
gcloud artifacts repositories create "$REPO" \
  --repository-format=docker \
  --location="$REGION" \
  --description="Quick Commerce container images" || true

echo "==> Creating Cloud SQL (PostgreSQL 16) instance — takes ~5 minutes"
gcloud sql instances create "$DB_INSTANCE" \
  --database-version=POSTGRES_16 \
  --tier=db-f1-micro \
  --region="$REGION" \
  --storage-auto-increase \
  --no-backup || true

echo "==> Creating database and user"
DB_PASSWORD=$(openssl rand -base64 24)
gcloud sql databases create "$DB_NAME" --instance="$DB_INSTANCE" || true
gcloud sql users create "$DB_USER" --instance="$DB_INSTANCE" --password="$DB_PASSWORD" || true

INSTANCE_CONNECTION=$(gcloud sql instances describe "$DB_INSTANCE" --format='value(connectionName)')
DATABASE_URL="postgresql://${DB_USER}:${DB_PASSWORD}@localhost/${DB_NAME}?host=/cloudsql/${INSTANCE_CONNECTION}"
echo ""
echo "  DB_PASSWORD (save this): $DB_PASSWORD"
echo "  INSTANCE_CONNECTION:     $INSTANCE_CONNECTION"
echo "  DATABASE_URL:            $DATABASE_URL"
echo ""

echo "==> Creating service account"
gcloud iam service-accounts create "$SA_NAME" \
  --display-name="Quick Commerce Cloud Run SA" || true
SA_EMAIL="${SA_NAME}@${PROJECT}.iam.gserviceaccount.com"

echo "==> Granting service account permissions"
gcloud projects add-iam-policy-binding "$PROJECT" \
  --member="serviceAccount:${SA_EMAIL}" \
  --role=roles/cloudsql.client
gcloud projects add-iam-policy-binding "$PROJECT" \
  --member="serviceAccount:${SA_EMAIL}" \
  --role=roles/secretmanager.secretAccessor
gcloud projects add-iam-policy-binding "$PROJECT" \
  --member="serviceAccount:${SA_EMAIL}" \
  --role=roles/run.invoker

echo "==> Storing secrets in Secret Manager"
echo "  -- Paste values when prompted. Press Ctrl+D when done."

store_secret() {
  local name=$1 value=$2
  echo -n "$value" | gcloud secrets create "$name" --data-file=- 2>/dev/null \
    || echo -n "$value" | gcloud secrets versions add "$name" --data-file=-
}

store_secret DATABASE_URL            "$DATABASE_URL"
store_secret DELIVERY_CODE_SECRET    "$(openssl rand -base64 32)"

echo ""
echo "  You still need to manually store these secrets (run the store_secret lines below):"
echo "  Replace <VALUE> with the real value, then paste into your terminal."
echo ""
echo "  echo -n '<VALUE>' | gcloud secrets create JWT_SECRET          --data-file=-"
echo "  echo -n '<VALUE>' | gcloud secrets create OTP_HASH_SECRET     --data-file=-"
echo "  echo -n '<VALUE>' | gcloud secrets create TWO_FACTOR_API_KEY  --data-file=-"
echo "  echo -n '<VALUE>' | gcloud secrets create GOOGLE_MAPS_PLATFORM_KEY --data-file=-"
echo "  echo -n '<VALUE>' | gcloud secrets create CLOUDINARY_CLOUD_NAME    --data-file=-"
echo "  echo -n '<VALUE>' | gcloud secrets create CLOUDINARY_API_KEY       --data-file=-"
echo "  echo -n '<VALUE>' | gcloud secrets create CLOUDINARY_API_SECRET    --data-file=-"
echo "  echo -n 'https://your-app.com' | gcloud secrets create ALLOWED_ORIGINS --data-file=-"
echo ""

echo "==> Updating cloudbuild.yaml substitutions"
sed -i '' \
  "s|_CLOUDSQL_INSTANCE: ''|_CLOUDSQL_INSTANCE: '${INSTANCE_CONNECTION}'|" \
  cloudbuild.yaml
sed -i '' \
  "s|_SA_EMAIL: ''|_SA_EMAIL: '${SA_EMAIL}'|" \
  cloudbuild.yaml
sed -i '' \
  "s|_ADMIN_PHONE: '.*'|_ADMIN_PHONE: '${ADMIN_PHONE}'|" \
  cloudbuild.yaml

echo ""
echo "==> Connecting Cloud Build to GitHub"
echo "  Go to: https://console.cloud.google.com/cloud-build/triggers/connect?project=$PROJECT"
echo "  Connect repo: muneerabass/quick-commerce-backend"
echo "  Then create a trigger:"
echo "    Branch: ^main$"
echo "    Config: cloudbuild.yaml"
echo "    Substitutions: leave defaults (already set in cloudbuild.yaml)"
echo ""
echo "==> Done. Commit the updated cloudbuild.yaml, push, then trigger a build."
echo "    git add cloudbuild.yaml && git commit -m 'chore: set GCP Cloud Run substitutions' && git push"
