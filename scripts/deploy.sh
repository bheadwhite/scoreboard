#!/usr/bin/env bash
#
# Deploy Scoreboard to Cloud Run.
#
#   ./scripts/deploy.sh                 build, sync secrets, deploy
#   ./scripts/deploy.sh --skip-secrets  deploy without touching Secret Manager
#   ./scripts/deploy.sh --scheduler     also (re)create the sweeper cron job
#
# Reads .env.deploy. See .env.deploy.example.
set -euo pipefail

cd "$(dirname "$0")/.."

ENV_FILE=".env.deploy"
[ -f "$ENV_FILE" ] || { echo "Missing $ENV_FILE — copy .env.deploy.example and fill it in."; exit 1; }

# shellcheck disable=SC1090
set -a; source "$ENV_FILE"; set +a

: "${GCP_PROJECT:?set GCP_PROJECT in .env.deploy}"
GCP_REGION="${GCP_REGION:-us-central1}"
SERVICE="${SERVICE:-scoreboard}"

SKIP_SECRETS=false
WITH_SCHEDULER=false
for arg in "$@"; do
  case "$arg" in
    --skip-secrets) SKIP_SECRETS=true ;;
    --scheduler)    WITH_SCHEDULER=true ;;
    *) echo "unknown flag: $arg"; exit 1 ;;
  esac
done

# Anything that grants access goes to Secret Manager; the rest are plain env
# vars, visible in the Cloud Run console where they are useful to see.
SECRET_KEYS=(DATABASE_URL SESSION_SECRET GOOGLE_CLIENT_SECRET SWEEP_TOKEN)
PLAIN_KEYS=(CONCEDE_WINDOW APP_URL GOOGLE_CLIENT_ID)

# Accept the GCP_OAUTH_* spelling as well, which is what the Google console
# calls them and what this repo's env file was first written with.
GOOGLE_CLIENT_ID="${GOOGLE_CLIENT_ID:-${GCP_OAUTH_CLIENT:-}}"
GOOGLE_CLIENT_SECRET="${GOOGLE_CLIENT_SECRET:-${GCP_OAUTH_SECRET:-}}"

: "${SESSION_SECRET:?set SESSION_SECRET in .env.deploy (openssl rand -hex 32)}"
: "${APP_URL:?set APP_URL in .env.deploy — the OAuth redirect URI is derived from it}"

# Never let gcloud stop to ask a question. Without this, a disabled API makes
# gcloud prompt "enable and retry?" on stdin — and if the caller has redirected
# output, that prompt is invisible and the script appears to hang.
export CLOUDSDK_CORE_DISABLE_PROMPTS=1

REQUIRED_APIS=(
  run.googleapis.com
  cloudbuild.googleapis.com
  secretmanager.googleapis.com
  artifactregistry.googleapis.com
)
[ "$WITH_SCHEDULER" = true ] && REQUIRED_APIS+=(cloudscheduler.googleapis.com)

echo "▸ Checking required APIs..."
ENABLED=$(gcloud services list --enabled --project "$GCP_PROJECT" --format="value(config.name)")
MISSING=()
for api in "${REQUIRED_APIS[@]}"; do
  grep -qx "$api" <<<"$ENABLED" || MISSING+=("$api")
done

if [ ${#MISSING[@]} -gt 0 ]; then
  echo "  Enabling: ${MISSING[*]}"
  echo "  (first time only; this can take a minute)"
  gcloud services enable "${MISSING[@]}" --project "$GCP_PROJECT"
fi
echo "  ✓ APIs ready"

# Fail before paying for a Cloud Build.
echo "▸ Checking the build locally first..."
pnpm --filter @scoreboard/frontend check
(cd backend && go vet ./... && go test ./...)

if [ "$SKIP_SECRETS" = false ]; then
  echo "▸ Syncing secrets..."
  for key in "${SECRET_KEYS[@]}"; do
    value="${!key:-}"
    [ -n "$value" ] || { echo "  – $key is empty, skipping"; continue; }

    if ! gcloud secrets describe "$key" --project "$GCP_PROJECT" >/dev/null 2>&1; then
      gcloud secrets create "$key" --project "$GCP_PROJECT" --replication-policy=automatic >/dev/null
      echo "  + created $key"
    fi
    printf '%s' "$value" | gcloud secrets versions add "$key" --project "$GCP_PROJECT" --data-file=- >/dev/null
    echo "  ✓ $key"
  done
fi

# Only bind secrets that actually exist, so an unset optional key does not
# break the deploy.
set_secrets=()
bound_keys=()
for key in "${SECRET_KEYS[@]}"; do
  if gcloud secrets describe "$key" --project "$GCP_PROJECT" >/dev/null 2>&1; then
    set_secrets+=("$key=$key:latest")
    bound_keys+=("$key")
  fi
done

# The Cloud Run *runtime* service account has to be able to read each secret it
# is given. This is not granted by default, and without it the container builds
# fine and then the revision fails to start — a confusing place to discover it.
# Granting per secret rather than project-wide keeps it least-privilege, and
# re-adding an existing binding is a no-op.
if [ ${#bound_keys[@]} -gt 0 ]; then
  echo "▸ Granting the runtime service account access to secrets..."
  PROJECT_NUMBER=$(gcloud projects describe "$GCP_PROJECT" --format='value(projectNumber)')
  RUNTIME_SA="${RUNTIME_SA:-${PROJECT_NUMBER}-compute@developer.gserviceaccount.com}"
  for key in "${bound_keys[@]}"; do
    gcloud secrets add-iam-policy-binding "$key" \
      --member="serviceAccount:${RUNTIME_SA}" \
      --role=roles/secretmanager.secretAccessor \
      --project "$GCP_PROJECT" >/dev/null
    echo "  ✓ $key"
  done
fi

set_env=()
for key in "${PLAIN_KEYS[@]}"; do
  value="${!key:-}"
  [ -n "$value" ] && set_env+=("$key=$value")
done

echo "▸ Deploying ${SERVICE} to ${GCP_REGION}..."
deploy_args=(
  run deploy "$SERVICE"
  --source .
  --project "$GCP_PROJECT"
  --region "$GCP_REGION"
  --allow-unauthenticated
  --port 8080
  # One instance, deliberately. The live change stream fans out in process, so
  # a second instance would have its own set of connections and neither would
  # see the other's events. containerConcurrency is 80, which is far more open
  # streams than a family will ever have — when that stops being true, the seam
  # to replace is internal/events, not this flag.
  --max-instances 1
  --min-instances 0
  # Streams are held open, and Cloud Run cuts a request at this deadline. The
  # server closes each stream a little earlier so reconnects are tidy.
  --timeout 600
  --cpu 1
  --memory 512Mi
)

[ ${#set_secrets[@]} -gt 0 ] && { IFS=','; deploy_args+=(--set-secrets "${set_secrets[*]}"); unset IFS; }
[ ${#set_env[@]} -gt 0 ]     && { IFS=','; deploy_args+=(--set-env-vars "${set_env[*]}"); unset IFS; }

gcloud "${deploy_args[@]}"

URL=$(gcloud run services describe "$SERVICE" --project "$GCP_PROJECT" --region "$GCP_REGION" --format='value(status.url)')
echo "▸ Live at $URL"

# The OAuth redirect URI is built from APP_URL, so a mismatch means every
# sign-in fails with redirect_uri_mismatch. Worth catching here rather than in
# a Google error page.
if [ "${APP_URL%/}" != "${URL%/}" ]; then
  echo
  echo "  ⚠ APP_URL is ${APP_URL} but the service is at ${URL}"
  echo "    Sign-in will fail until they match. Update .env.deploy and redeploy."
fi
echo "    Google console authorized redirect URI: ${URL}/auth/google/callback"

if [ "$WITH_SCHEDULER" = true ]; then
  : "${SWEEP_TOKEN:?set SWEEP_TOKEN in .env.deploy to schedule the sweeper}"
  JOB="$SERVICE-sweep"

  # This one job does two things: it finalizes claims whose concession window
  # has closed, and the request itself keeps a free-tier Supabase project from
  # being paused for inactivity.
  echo "▸ Scheduling the sweeper every 15 minutes..."
  action=create
  gcloud scheduler jobs describe "$JOB" --project "$GCP_PROJECT" --location "$GCP_REGION" >/dev/null 2>&1 && action=update

  # create and update do not share a spelling for this: create takes --headers,
  # update takes --update-headers. Getting it wrong only shows up on the second
  # deploy, once the job already exists.
  if [ "$action" = create ]; then
    header_flag=--headers
  else
    header_flag=--update-headers
  fi

  gcloud scheduler jobs "$action" http "$JOB" \
    --project "$GCP_PROJECT" \
    --location "$GCP_REGION" \
    --schedule "*/15 * * * *" \
    --uri "$URL/tasks/sweep" \
    --http-method POST \
    "$header_flag" "Authorization=Bearer $SWEEP_TOKEN" \
    --attempt-deadline 120s >/dev/null
  echo "  ✓ $JOB ($action)"
fi

echo "✅ Done."
