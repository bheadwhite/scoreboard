# 🥊 Gauntlet

A scoreboard for everything you play. Everybody who signs in is on the same
board — friends, family, whoever you play with — and the app keeps a permanent
record of who beat whom at what.

The point is bragging rights. The headline stat is not "your win rate" — it is
**"you have beaten Dad at Rook 14 times and he has beaten you 3 times."**

## The mechanic

**Anybody can declare a win. Everybody can say it did not happen.**

Somebody names the winner and the result is on the board — no lobby, no ready
check, no pending stage, no clock. What keeps the board honest is not a delay in
front of a result, it is that every result stays challengeable behind it: anyone
who was in the match can object to it, at any point in its life including years
later, with nobody's permission and no majority needed. A contested result is
held out of every tally on the scoreboard — wins, losses, streaks, rivalries,
game kings — while staying completely visible as a match, with the objector's
name and reasons on it. It stops being contested the moment they take it back.

There was a week-long concession window here once, and taking it out is what the
rule above is. It changed nothing: an unanswered claim settled exactly as
claimed and a unanimously rejected one settled exactly as claimed and contested,
so the wait was a delay rather than a check. What actually protected the record
was the objection, and the objection works just as well on a settled match.

Nothing anybody says is a one-way door. Every answer is a position held, not a
vote spent — concede and you can still object, object and you can still concede
— and the side a result credits can refuse it outright, which takes it back off
the board. Nobody can erase a loss, only refuse a win.

**Nobody adjudicates.** There is no dispute resolution, no referee and no
deadlock, because an objection cannot deadlock anything.

```
IN_PROGRESS ──somebody names the winner──> FINAL      counts immediately
     ↑                                       │
     └──── the winning side refuses it ──────┘

        FINAL + any standing objection = contested, counts for nobody
                                          (reversible, forever)
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
make dev-web    # Vite on :3000
```

Local development needs no Google credentials: set `AUTH_DEV_LOGIN=true` in
`.env.local` and sign in at `/auth/dev/login?name=Bill`. The server refuses to
enable that route in production.

Copy `.env.local.example` to `.env.local` and fill in a `DATABASE_URL`.

## Docs

| File | What is in it |
|---|---|
| [CLAUDE.md](CLAUDE.md) | Conventions, architecture, and the things that will bite you |
| [DESIGN.md](DESIGN.md) | Domain model and schema. Written against an older protocol — read CLAUDE.md for what the code does now |
| [DEPLOY.md](DEPLOY.md) | Cloud Run deployment, OAuth setup, the keepalive cron |
| [DESIGN_BRIEF.md](DESIGN_BRIEF.md) | Visual design brief |

## Commands

```bash
make check                         # typecheck + test everything
make generate                      # regenerate Go + TS from the protos
make data                          # show what is in the database
make clear-family FAMILY="name"    # delete one family and everything under it
make consolidate                   # plan for folding every family into one
make deploy                        # Cloud Run
```
