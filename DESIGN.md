# Gauntlet — Design

Gauntlet is a scoreboard for everything you play. Everybody who signs in is on
the same board, plays games, and a win only counts once the losers concede it. The point of the app is bragging rights: how many times
you've beaten a *specific person* at a *specific game*.

The integrity mechanic is the product. Nobody self-reports a win into the record —
you claim victory, and the win becomes real only when the opposing side acknowledges
it. That's what makes the numbers worth arguing about.

## Decisions

| Question | Decision |
|---|---|
| Concession rule | Unanimous, with auto-concede after a timeout |
| Ranking | Raw W/L + head-to-head (store raw results; ratings can come later) |
| Auth | Supabase Auth, JWT verified in Go |
| Database | Supabase Postgres + a keepalive cron |
| Transport | Protobuf over Connect RPC |
| Deploy | Single Cloud Run service, Go binary serves the SPA |

## Reference implementations

This repo follows conventions already proven in your other projects. Copy, don't invent:

- **Go + buf + Connect + Cloud Run** — `/Users/bdub/projects/tkapp` (Postgres, embedded
  migrations, `/api` mount, SPA fallback) and `/Users/bdub/projects/wallet`.
- **Presenter pattern + domain cache** — `/Users/bdub/projects/wallet/frontend/src/lib/domain/`
  (`domain.ts`, `store.ts`, `collection.ts`) is exactly the "core domain holding the
  caching layer, injected into every presenter" model. `KeyedCollection` comes from
  `/Users/bdub/projects/sesh/src/state/store/Collection.ts`.
- **Rationale doc** — `/Users/bdub/projects/doorstep/PRESENTERS.md`.
- **Deploy script** — `/Users/bdub/projects/honeydo/scripts/deploy.sh`.

---

## Domain model

### The contract

A **contract** is the victory condition, declared *before* play begins and locked once
the match starts. It exists so the win condition can't be relitigated after someone
wins ("I thought we were playing to 500!").

```
contract_kind: FIRST_TO_SCORE | BEST_OF | LAST_STANDING | SINGLE_GAME | CUSTOM
contract_target: int          // 500, 3, …  (null for LAST_STANDING/SINGLE_GAME)
contract_detail: string       // free text, always shown; required for CUSTOM
```

### Everything is a team

Free-for-all games give each player a singleton team. This collapses "concede to that
player or that team" into one code path and one schema. No special-casing solo play.

### Match lifecycle

```
   LOBBY ──start──> IN_PROGRESS ──claim──> CLAIMED ──all concede──> FINAL
     │                                        │       or timeout
     │                                        │
  cancel                                   dispute
     ↓                                        ↓
  CANCELED                                DISPUTED ──owner resolves──> FINAL | CANCELED
```

Only `FINAL` matches touch the scoreboard.

**Claim.** Any participant claims victory for their team. Sets
`concede_deadline = now() + 24h`. The claimant's own teammates are auto-conceded —
only opposing participants owe a response.

**Concede.** Marks that participant conceded. When every opposing participant has
conceded, the match finalizes immediately.

**Timeout.** If the deadline passes with outstanding concessions, the match finalizes
anyway. This is what stops one holdout from freezing the record.

**Dispute.** An opposing participant may dispute instead of concede. This *stops the
clock* and moves the match to `DISPUTED`, where only the family owner can resolve it.
Without this, "auto-concede after 24h" would let a genuinely false claim silently
become truth — silence and disagreement must not mean the same thing.

### Schema

```sql
-- id mirrors Supabase auth.users.id; row is upserted on first authenticated call
users              (id, display_name, avatar_url, created_at)

families           (id, name, invite_code unique, owner_id, created_at, is_default)
family_members     (family_id, user_id, role, joined_at)          -- pk(family_id, user_id)

-- family_id null = global catalog game (Rook, Catan, …), seeded at migration time
games              (id, family_id, name, team_based, min_players, max_players,
                    created_by, created_at)

matches            (id, family_id, game_id, status,
                    contract_kind, contract_target, contract_detail,
                    created_by, started_at,
                    claimed_at, claimed_by_team_id, concede_deadline,
                    finalized_at, created_at)

match_teams        (id, match_id, name, ordinal)
match_participants (match_id, user_id, team_id, result, conceded_at, disputed_at)
                                                                   -- pk(match_id, user_id)
```

`result`: `PENDING | WIN | LOSS`, written at finalization.

Indexes: `match_participants(user_id)`, `match_participants(match_id)`,
`matches(family_id, game_id, status)`, `matches(status, concede_deadline)` for the sweeper.

### The query the whole app exists for

Head-to-head is a self-join on `match_participants` across opposing teams:

```sql
SELECT a.result, count(*)
FROM match_participants a
JOIN match_participants b
  ON b.match_id = a.match_id AND b.team_id <> a.team_id
JOIN matches m ON m.id = a.match_id
WHERE m.family_id = $1
  AND m.status = 'FINAL'
  AND a.user_id = $2 AND b.user_id = $3
  AND ($4::uuid IS NULL OR m.game_id = $4)
GROUP BY a.result;
```

**Compute standings on read.** At family scale — a few thousand matches, ever — there
is no reason to materialize a standings table. Cache the result in a frontend
`KeyedCollection` and invalidate on match finalization.

Derived stats that fall out of this data for free, and are the actual bragging payoff:
current win streak, longest streak, and **nemesis** (the opponent you've lost to most
at a given game).

---

## Backend

Module `github.com/bheadwhite/scoreboard/backend`, Go 1.25. Layout per `tkapp`:
`cmd/server` + `internal/{auth,service,store,model}` + committed `gen/`. stdlib
`net/http` + `connectrpc.com/connect` + `h2c`. No framework, no slog, env config via a
3-line `env()` helper.

### Services

```
UserService       GetMe, UpdateProfile
FamilyService     CreateFamily, JoinFamily(inviteCode), ListMyFamilies, GetFamily,
                  ListMembers, RemoveMember, RegenerateInviteCode
GameService       ListGames(familyId), CreateGame
MatchService      CreateMatch, JoinMatch, SetTeams, SetContract, StartMatch,
                  ClaimVictory, Concede, DisputeMatch, ResolveDispute,
                  CancelMatch, GetMatch, ListMatches
StatsService      GetStandings(familyId, gameId?, range), GetHeadToHead(familyId, a, b, gameId?),
                  GetPlayerProfile(familyId, userId)
```

### Auth

Frontend signs in with `@supabase/supabase-js` and holds the session. A Connect
interceptor attaches `Authorization: Bearer <jwt>`. A Go interceptor in
`internal/auth` validates the token and puts the user id in the request context;
every handler reads the caller from context and never trusts a user id off the wire.

> **Verify before implementing:** Supabase historically signed JWTs with HS256 using
> the project JWT secret; newer projects use asymmetric keys published at
> `https://<ref>.supabase.co/auth/v1/.well-known/jwks.json`. Check which this project
> uses and pick `golang-jwt/jwt/v5` alone (HS256) or with `MicahParks/keyfunc/v3` (JWKS).

Authorization is family-scoped: every read and write asserts the caller is a member of
the family that owns the resource. Owner-only: `RemoveMember`, `ResolveDispute`,
`RegenerateInviteCode`.

### Connection pooling — non-negotiable for Cloud Run

Use Supabase's **transaction pooler on port 6543**, not the direct connection. Cap
`pgxpool` at 2–4 connections per instance, and disable prepared-statement caching,
which transaction poolers don't support:

```go
cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
cfg.MaxConns = 4
```

(Same footgun as `prepare: false` in `biz_huntr/lib/db.ts:19`.)

### The sweeper cron does two jobs at once

A Cloud Scheduler job hits an authenticated `POST /tasks/sweep` every 15 minutes. It
finalizes `CLAIMED` matches past their `concede_deadline` — **and** that same request
keeps the Supabase project from hitting the free-tier inactivity pause. One cron, both
problems.

Finalization is also evaluated lazily on any read of a `CLAIMED` match, so the record
is correct even if the cron is down.

### Migrations

`//go:embed *.sql` applied lexically on boot, written idempotent (`IF NOT EXISTS`) —
per `/Users/bdub/projects/tkapp/backend/migrations/embed.go`.

---

## Frontend

Vite + React 18 + pnpm, scoped `@scoreboard/frontend`, snake_case file names per
`wallet`. **Mobile-first** — this is used standing around a card table, so big tap
targets, especially on Concede. Installable PWA.

### Structure

```
src/
  main.tsx                     composition root: create domain + AppPresenter outside React
  app/app_presenter.ts         owns every feature presenter, route state, refreshAll()
  app/app_context.tsx          root-only context (wallet's Style B)
  app/app_view.tsx
  lib/clients.ts               Connect clients + auth interceptor
  lib/domain/domain.ts         { api, store, session }
  lib/domain/store.ts          the cache registry
  lib/domain/collection.ts     copy from wallet
  lib/domain/keyed_collection.ts  adapted from sesh
  lib/key_promise_cache.ts     copy from wallet
  features/<name>/<name>_presenter.ts + <name>_view.tsx
  gen/scoreboard/v1/
```

`Domain` gains a third member beyond wallet's `{api, store}`: a `session` holding the
Supabase client and the current user, so presenters can read the viewer without
importing auth.

### Store collections

```ts
families    Collection<Family[]>
games       KeyedCollection<familyId, Game[]>
matches     KeyedCollection<familyId, MatchSummary[]>
standings   KeyedCollection<`${familyId}|${gameId}|${range}`, Standings>
headToHead  KeyedCollection<`${familyId}|${a}|${b}|${gameId}`, HeadToHead>
```

`store.invalidateAll()` on match finalization — the standings and head-to-head cells
are exactly what goes stale.

### Presenter rules (from your existing CLAUDE.mds — carry them into this repo's)

- Private `Signal`/`Runner` fields, public `get xBroadcast()` getters, mutators, `dispose()`.
- Never expose a `Signal`/`Runner` — only `.broadcast`.
- Every async read is `Runner.execute(...)`. Never a `loading: boolean`.
- Callback-shaped methods are arrow properties.
- Store the return of `.subscribe()` — subscriptions are `WeakRef`-backed and will be
  silently GC'd otherwise.
- Domain owns the data cache; presenters own async operation state.
- A presenter disposes what it owns and nothing else. Prop-injected presenters are
  never disposed by components.

### Live updates: poll, don't stream

`@tcn/state` ships `PollAsync`. Poll the active match every 3s while status is
`LOBBY | IN_PROGRESS | CLAIMED`; stop on `FINAL`. This is free, trivial, and survives
bad wifi. Connect server-streaming buys nothing at this scale.

### Idempotency

`ClaimVictory` and `Concede` must be safe to call twice — double-taps on a flaky
connection are the normal case, not the edge case. Both are naturally idempotent if
written as "set this participant's `conceded_at` if null."

---

## Deploy

Multi-stage Dockerfile → distroless, Go binary serves the built SPA (same-origin, so
CORS never applies), single Cloud Run service. `scripts/deploy.sh` doing
`gcloud run deploy --source .`, secrets synced into Secret Manager from `.env.deploy`.
Pattern: `/Users/bdub/projects/tkapp/Dockerfile` + `/Users/bdub/projects/honeydo/scripts/deploy.sh`.

Secrets: `DATABASE_URL` (pooler URI, port 6543), `SUPABASE_JWT_SECRET` or project ref,
`SWEEP_TOKEN`. Build-time frontend vars: `VITE_SUPABASE_URL`, `VITE_SUPABASE_ANON_KEY`.

Carry over the billing cap from `/Users/bdub/projects/sesh/infra/billing-cap/`.

Testing: `go test ./...` beside the code, vitest+jsdom configured inline in
`vite.config.ts`, aggregated by `make test`.

---

## Build order

1. **Skeleton** — repo layout, buf codegen, Go server, Vite app, `make dev` working end to end.
2. **Auth + families** — Supabase sign-in, JWT interceptor, create/join family, member list.
3. **Games + matches** — catalog, lobby, teams, contract, start.
4. **The concession protocol** — claim, concede, dispute, timeout sweeper. The heart of it.
5. **Scoreboard** — standings, head-to-head, streaks, nemesis.
6. **Deploy** — Dockerfile, Cloud Run, scheduler, billing cap.
