# 🏆 Scoreboard

A family game scoreboard. Players join a family, log the games they play, and the
app keeps a permanent record of who beat whom at what.

The point is bragging rights. The headline stat is not "your win rate" — it is
**"you have beaten Dad at Rook 14 times and he has beaten you 3 times."**

## The mechanic

**Nobody can declare their own win.**

You claim victory, and the claim sits publicly pending until every opponent
concedes. Silence times out into a concession after 24 hours. An explicit
dispute stops the clock and waits for the family organizer.

Only ratified matches touch the scoreboard — which is what makes the numbers
worth arguing about.

```
LOBBY → IN_PROGRESS → CLAIMED → FINAL        all opponents concede, or timeout
                         ↓
                     DISPUTED → FINAL | VOID  organizer resolves
```

## Stack

TypeScript · React 18 · Vite · Go 1.25 · Connect RPC over protobuf · Postgres ·
pnpm. Deployed to Cloud Run as a single container where the Go binary serves the
SPA, so everything is same-origin.

Auth is hand-rolled Google OAuth with HMAC-signed HttpOnly session cookies — no
token ever reaches JavaScript.

## Quick start

```bash
make setup      # tools, dependencies, local database
make dev-api    # backend on :8080
make dev-web    # Vite on :5173
```

Local development needs no Google credentials: set `AUTH_DEV_LOGIN=true` in
`.env.local` and sign in at `/auth/dev/login?name=Bill`. The server refuses to
enable that route in production.

Copy `.env.local.example` to `.env.local` and fill in a `DATABASE_URL`.

## Docs

| File | What is in it |
|---|---|
| [CLAUDE.md](CLAUDE.md) | Conventions, architecture, and the things that will bite you |
| [DESIGN.md](DESIGN.md) | Domain model, schema, and why the concession protocol works this way |
| [DEPLOY.md](DEPLOY.md) | Cloud Run deployment, OAuth setup, the sweeper cron |
| [DESIGN_BRIEF.md](DESIGN_BRIEF.md) | Visual design brief |

## Commands

```bash
make check                         # typecheck + test everything
make generate                      # regenerate Go + TS from the protos
make data                          # show what is in the database
make clear-family FAMILY="name"    # delete one family and everything under it
make deploy                        # Cloud Run
```
