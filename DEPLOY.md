# Deploying Scoreboard

One Cloud Run service. The Go binary serves the built SPA, so everything is
same-origin and CORS never enters the picture.

---

## 1. Supabase

Create a project. You need two things from it.

### The database URL

**Dashboard → Project Settings → Database → Connection string → Transaction pooler**

It looks like:

```
postgres://postgres.<ref>:<password>@aws-0-<region>.pooler.supabase.com:6543/postgres
```

**Use the transaction pooler on port 6543, not the direct connection on 5432.**
Cloud Run creates many short-lived instances, each with its own pool, and the
free tier's connection ceiling is shared across all of them. The direct
connection will run out.

The backend is already configured to match: prepared statements are disabled
(a transaction pooler hands out a different backend connection per transaction,
so a named prepared statement created on one is missing on the next) and
`MaxConns` is capped at 4. See `backend/internal/store/store.go`.

The schema is applied automatically on boot from `backend/migrations/`. There is
nothing to run by hand.

### That is all Supabase is for

Supabase is only a Postgres host here. Authentication is this app's own Google
OAuth flow — there is no Supabase Auth, no anon key, and nothing to configure in
the Supabase dashboard beyond the database.

---

## 1b. Google OAuth

**Google Cloud console → APIs & Services → Credentials → OAuth 2.0 Client IDs.**
Create one (or reuse an existing one) and add the callback to its **Authorized
redirect URIs**:

```
https://YOUR-SERVICE-URL/auth/google/callback
http://localhost:3000/auth/google/callback     # for local development
```

That URI is derived from `APP_URL`, so the two must agree exactly — a mismatch
fails every sign-in with `redirect_uri_mismatch`. You only learn the deployed URL
after the first deploy, so expect to set `APP_URL` and redeploy once. The script
prints the exact URI to register and warns if `APP_URL` has drifted.

Also set `SESSION_SECRET` (`openssl rand -hex 32`). It signs the session
cookies; changing it logs everyone out.

---

## 2. Google Cloud

```bash
gcloud auth login
gcloud config set project YOUR_PROJECT
gcloud services enable run.googleapis.com cloudbuild.googleapis.com \
  secretmanager.googleapis.com cloudscheduler.googleapis.com
```

---

## 3. Deploy

```bash
cp .env.deploy.example .env.deploy   # then fill it in
./scripts/deploy.sh --scheduler
```

The script typechecks and tests locally first (so you do not pay for a Cloud
Build that was going to fail), syncs `DATABASE_URL`, `SUPABASE_JWT_SECRET` and
`SWEEP_TOKEN` into Secret Manager, deploys from source, and — with `--scheduler` —
creates the sweeper cron job.

Useful flags:

```bash
./scripts/deploy.sh --skip-secrets   # code-only redeploy
./scripts/deploy.sh --scheduler      # also (re)create the cron job
```

---

## 4. The sweeper cron — do not skip this

```
POST /tasks/sweep     every 15 minutes, Authorization: Bearer $SWEEP_TOKEN
```

It does **two** jobs:

1. **Finalizes claims whose 24-hour concession window has closed.** Without it,
   a match where somebody never responds stays pending forever. (Matches are
   also finalized lazily whenever they are read, so the record stays correct if
   the cron is down — but only for matches somebody actually looks at.)
2. **Keeps the Supabase project awake.** A free-tier project is paused after a
   stretch of inactivity, and a family scoreboard is exactly the kind of app
   that goes quiet for two weeks and then gets used hard on Thanksgiving. A
   request every 15 minutes prevents that.

`--scheduler` sets it up. Verify with:

```bash
gcloud scheduler jobs list --location us-central1
```

---

## 5. Verify

```bash
curl -fsS https://YOUR-SERVICE-URL/health      # pings the database, prints "ok"
```

Then sign in, create a family, and play a game through to a concession.

---

## Operating notes

**Cost.** `--max-instances 4` caps concurrent instances, which also caps database
connections at 16. `--min-instances 0` means scale to zero, so a cold start costs
a second or two on the first request after a quiet spell. That is the right trade
for a family app. Consider a billing budget alert.

**Changing the concession window.** `CONCEDE_WINDOW` is a Go duration (`168h`,
`30m`), defaulting to a week. It is a backstop for a match everybody walked away
from, not a deadline players are meant to race: an unanswered claim stays on
everyone's list the whole time, and any player can change their answer until it
finalizes. Shortening it to a day makes an ordinary weekend away ripen into a
concession nobody gave.

It applies to newly claimed matches; already-claimed matches keep the deadline
they were given. Withdrawing the last dispute on a match restarts the clock with
a full window, because the players who were still silent had nothing to answer
while the argument was live.

**Rollback.**

```bash
gcloud run services update-traffic scoreboard --to-revisions PREVIOUS=100
```

**Migrations** are idempotent and re-run on every boot, so a rollback is safe as
long as the older binary understands the newer schema. Only add columns; never
drop or rename one in the same release that stops using it.
