# Gauntlet

A scoreboard for everything you play. Everybody who signs in is on the same
board — friends, family, whoever you play with — and the app keeps a permanent
record of who beat whom at what. The point is bragging rights: the headline stat
is not "your win rate", it is "you have beaten Dad at Rook 14 times and he has
beaten you 3 times."

**The product is called Gauntlet; the code is not.** The repository, the Go
module (`github.com/bheadwhite/scoreboard`), the proto package (`scoreboard.v1`),
the pnpm workspace (`@scoreboard/frontend`) and the Cloud Run service all still
say scoreboard, deliberately — renaming the proto package regenerates both
clients for nothing, and renaming the Cloud Run service changes the URL, which
`APP_URL` and the OAuth redirect URI are both derived from. The name lives in
what a player sees: the title, the manifest, the sign-in hero, the share sheet.

**The mechanic that defines the app: nobody can declare their own win.** You
claim victory, and the claim sits publicly pending until every opponent
concedes. Only ratified matches touch the scoreboard, and that is what makes the
numbers worth arguing about.

Nothing about a pending claim is a one-way door. Every answer is a *position
held*, not an event logged: an opponent can move between conceding and objecting
for as long as the match is open, a claimant can withdraw the claim outright, and
the match sits on every player's list showing what they currently say. Silence
eventually reads as a concession, but the window is a week — a backstop for a
match everybody walked away from, not a countdown anyone is meant to race. An
objection stops even that. The family organizer can break a deadlock, but only
once the players genuinely cannot move it themselves; they are a last resort, not
a referee.

## Stack

TypeScript + React 18 + Vite (frontend) · Go 1.25 + Connect RPC (backend) ·
protobuf over Connect · Postgres (Supabase) · pnpm · deployed to Cloud Run as a
single container where the Go binary serves the SPA.

## Layout

```
proto/scoreboard/v1/     the contract, source of truth for both sides
backend/
  cmd/server/            wiring only
  internal/auth/         Google OAuth + signed session cookies (Connect interceptor)
  internal/service/      Connect handlers — thin; mappers.go does proto conversion
  internal/store/        persistence, one file per aggregate
  internal/stats/        pure functions: standings, head-to-head, streaks, nemesis
  internal/model/        domain types
  migrations/            embedded SQL, idempotent, applied on boot
  gen/                   generated Go — COMMITTED
frontend/src/
  app/                   root presenter, context, shell
  lib/domain/            the injected core: { api, store, session }
  features/<name>/       <name>_presenter.ts + <name>_view.tsx
  gen/                   generated TS — COMMITTED
```

## Commands

```
make setup       tools, deps, local database
make generate    regenerate Go + TS from the protos
make dev-api     backend on :8080
make dev-web     Vite on :3000, proxying /api to :8080
make check       typecheck + test everything
make deploy      Cloud Run (see DEPLOY.md)

make data                        show what is in the database
make clear-family FAMILY="name"  delete one family and everything under it
make clear-all                   delete every family, match and player

make consolidate                 plan for folding every family into one
make consolidate-apply           actually fold them together
```

`backend/cmd/reset` is the maintenance CLI behind those last three. It is a dry
run unless you pass `-yes`, it prints what it would delete first, and it never
touches the built-in game catalog — that is schema, not data. Use it when test
families end up in a database you also mean to use for real.

`backend/cmd/consolidate` is the other one, and the other half of the app being
single-family. It moves every other family's members, matches and games into one
— `-into "<name or id>"`, or the family sign-in already uses — collapses custom
games that are now duplicates, and deletes the emptied families. Dry run unless
you pass `-yes`, safe to point at production, and nothing leaves the permanent
record: the destination keeps its id, name and invite code, so no session and no
link breaks. Run it once on a database that grew up multi-family, right after
the deploy that carries the migration: until it runs, sign-in puts people in
whichever family the migration found oldest, which may not be the one you want
everybody in.

Local development works without Google: set `AUTH_DEV_LOGIN=true` in
`.env.local` and sign in at `/auth/dev/login?name=Bill`. The server refuses to
enable that route in production.

## State management — `@tcn/state`, presenter pattern

Non-negotiable conventions, carried over from the other projects:

- Presenters are plain classes: private `Signal`/`Runner` fields, public
  `get xBroadcast()` getters, mutator methods, and `dispose()`.
- **Never expose a `Signal` or `Runner`** — only `.broadcast`.
- Every async read is a `Runner.execute(...)`. There is never a `loading: boolean`.
- **Store the return of `.subscribe()`.** Subscriptions are `WeakRef`-backed and
  will be silently garbage collected otherwise.
- Prefer `transform()` over `set()` when editing arrays or objects in place.
- Callback-shaped methods are arrow properties, so they survive being passed down.
- Import from `@tcn/state/core` outside React, `@tcn/state/react` in components.
- `derive()` batches on a microtask, so tests must `await` rather than assert
  synchronously.
- A presenter disposes what it owns and **nothing it does not**. Store
  collections are owned by the store. Prop-injected presenters are never
  disposed by components.

`@tcn/state@1.3.4` shows as deprecated on npm. That is expected. Do not remove it.

## The domain / caching layer

`Domain = { api, store, session }` is created once outside React and injected
into every presenter.

- `domain.store.<collection>` — cached, shared, invalidatable reads
- `domain.api.<service>.<rpc>` — writes, and one-off reads
- `domain.session` — who is signed in

`Collection` is the cache primitive: a `Runner` plus a staleness flag plus
in-flight coalescing. `KeyedCollection` is a family of them keyed by a composite
string (standings per family/game/range, head-to-head per pair). Composite keys
are built by the `key` helpers in `store.ts` so reads and invalidation cannot
drift apart.

When a match finalizes, call `store.invalidateAll()` — standings, every
head-to-head record and several profiles all change at once, and it is not worth
reasoning about which.

**Bind views with `useCollection(cell)`, never `useSignalValue(cell.broadcast)`.**
Reading the broadcast directly renders fine but never registers with the
Collection, and `invalidate()` only refetches a cell something is watching. Get
this wrong and every `invalidateAll()` in the app becomes a silent no-op: cells
are marked stale and nothing reloads them, so the UI shows first-fetch data until
a full page refresh. That is exactly the bug that made joining a family require a
restart. `collection.test.ts` pins the behavior.

## Things that will bite you

**Postgres behind a transaction pooler.** Production connects to Supabase on
port **6543**, not 5432. Prepared statements are disabled
(`QueryExecModeSimpleProtocol`) and `MaxConns` is 4, because a pooler hands out a
different backend per transaction and Cloud Run multiplies instances. Changing
either will produce "prepared statement does not exist" under load.

**One family, and nothing in the UI says so.** The app was multi-family and is
not any more: there is no onboarding, no invite prompt, no switcher, no family
name on any screen. Rows are still scoped by `family_id` — collapsing that would
rewrite every match and every stats query to buy nothing — so instead exactly one
family carries `families.is_default`, and `EnsureDefaultMembership` puts every
signing-in player into it, minting it (owned by the first person through, named
`FAMILY_NAME`) if the database is empty. That call lives in the auth `lookup`
closure in `cmd/server/main.go`, which is the whole of onboarding now. Two
consequences worth knowing: *anyone who can sign in is on the scoreboard*, so the
invite code is how people are told the app exists rather than what admits them;
and a player whose session predates their membership is not fixed by a reload —
the shell says so instead of showing an empty scoreboard. `FamilyService` still
carries `CreateFamily` and `JoinFamily`; nothing calls them, and a second family
made behind the app's back would be invisible to it.

**Losing the same free-for-all is not a head-to-head result.** Every player gets
a team of one when a game has no sides, so "on opposing teams" is true of
everybody who did not win — and the obvious reading of that (you lost, they were
opposite you, therefore they beat you) records a defeat to somebody who lost the
same game. `HeadToHead` and `Rivalries` both skip a meeting where neither player
won, and they have to agree: one of them counting it would put a rivalry on a
profile that the rivalry screen then shows as 0–0. It is not a result, not a
streak, and not a row in recent meetings.

**The rivalry stats are absent until they are earned.** The drought, the
unfinished business and the game kings are all optional fields, and `stats`
returns them with a `false` rather than a zero value when nothing qualifies —
two meetings minimum (`minMeetings`), a win before anybody owns a game. That is
deliberate: "0 meetings since you last beat Dad" and a crown handed to somebody
for one game are both worse than silence, and a screen that pads itself with
non-facts stops being worth reading. Every one of them comes off the same
`Rivalries()` pass over the same finalized participations, so adding another
question of the form "who have I not beaten lately" costs a function, not a
query.

**Every participant always has a team**, including in free-for-all games, where
each player gets a team of one. This collapses "concede to a player" and
"concede to a team" into one code path. Do not add a parallel non-team path.

**A standing objection clears `concede_deadline`.** That is the whole mechanism
separating disagreement from silence. If you ever leave the deadline set on a
disputed match, a false claim will silently become true once the window runs out.

**`CLAIMED` and `DISPUTED` are derived, not latched.** They are the same
situation — a claim on the table, a set of answers to it — read with and without
an objection in the pile, so `settleAnswers()` recomputes the status from the
answers after every change rather than flipping it one way when somebody first
objects. Withdrawing the last objection therefore returns the match to `claimed`
and restarts the clock with a *full* window: the players who had said nothing
were watching an argument, not sitting on an unanswered claim. Write a status
directly in a handler and a match will get stuck disputed with its clock stopped
forever.

**`Concede` accepts a disputed match on purpose.** That is how somebody takes
their own objection back. Narrow it to `claimed` and the only way out of a
dispute becomes the organizer.

**The organizer is gated on `Match.Deadlocked(window)`**, not on "is this
disputed" — everyone has answered and they still disagree, or the claim has
outlived its window. The second test is not optional: an objection stops the
clock, so a disputer who goes quiet would otherwise freeze the match with no way
to finish it. `ResolveDispute` enforces this server-side; the proto field
`organizer_may_resolve` is only how the UI knows.

**Voiding is the only exit from the permanent record, and it is a hard delete.**
`MatchService.VoidMatch` erases the row; participants and teams go with it. That
is deliberate — a voided match kept as a row would be a fourth status every stats
query has to remember to skip. Because stats are computed on read, deleting the
row *is* the whole cleanup: standings, head-to-head and profiles simply stop
counting it. It is gated on `ADMIN_EMAILS` (a service administrator, checked
against `users.email`), **not** on any family role, and unlike every other match
handler it does not require family membership — running the deployment is the
authorization. Organizers get `ResolveDispute`, which still names a winner.

**Finalization happens in two places** — `finalizeIfRatified` (last concession
lands) and `finalizeIfExpired` (clock ran out, checked lazily on read *and* by
the sweeper). Both funnel through `finalize()`, which is the only place a match
enters the permanent record, and finalizing is the *only* irreversible step in
the whole protocol.

**`POST /tasks/sweep` does double duty**: it finalizes timed-out claims and the
request itself keeps a free-tier Supabase project from being paused for
inactivity. Cloud Scheduler hits it every 15 minutes.

**The health endpoint is `/health`, not `/healthz`.** Google Front End reserves
`/healthz` on `*.run.app` and answers it with its own 404 before the request
reaches the container — it works locally and silently does not in production.
It also has to stay a *registered* route: any unregistered path falls through to
the SPA handler and returns 200 with index.html, which looks like a healthy
service while checking nothing.

**Auth is hand-rolled, and no token ever reaches JavaScript.** The server runs
the Google OAuth flow itself (`internal/auth`), then issues an HttpOnly cookie
holding an HMAC-signed session. Signing in is a full-page redirect to
`/auth/google/start`; the browser attaches the cookie to same-origin requests by
itself, which is why `clients.ts` has no auth interceptor. Supabase is only a
Postgres host — the same way it is used in every other project here.

`APP_URL` must exactly match the origin the app is served from, because the
OAuth redirect URI is derived from it. A mismatch fails every sign-in with
`redirect_uri_mismatch`; the deploy script warns when it drifts.

**The update banner is a pair, and both halves come from the same build.**
`vite.config.ts` stamps one timestamp into two places: `__BUILD_ID__`, frozen
into the bundle by `define`, and `dist/version.json`, emitted as an asset and
served by the Go binary as an ordinary static file. The app fetches that file
`no-store` and offers a reload when the two disagree. Nothing in Go knows about
any of it. Two things to keep in mind: it is a *timestamp* rather than a commit
because `.git` is in `.dockerignore` and the Cloud Build container has no
repository to ask; and `/version.json` is deliberately **not** in the Vite dev
proxy, so in development it resolves to index.html, fails to parse, and reads as
"no update" — proxying it to a stale `dist/` would show the banner permanently.
Any deploy of a change to this mechanism reaches open tabs one deploy late,
because the tab has to already be running the checking code to notice.

**Frontend runtime config is injected, not compiled in.** `index.html` ships
with `__GOOGLE_ENABLED__` / `__DEV_LOGIN__` placeholders that `spaHandler`
replaces at startup, and `lib/runtime_config.ts` falls back to dev defaults when
it sees an unsubstituted placeholder. These are capability flags only — there
are deliberately no credentials in the bundle.

**Stats are computed on read, never materialized.** The store does one query for
a family's finalized participations and `internal/stats` does the rest as pure
functions. At family scale that is correct and it makes all the interesting
logic unit-testable without a database.

## Testing

Go tests live beside the code (`go test ./...`); the stats package carries the
real coverage. Frontend uses vitest + jsdom, configured inline in
`vite.config.ts`, with tests as `*.test.ts(x)` next to their source.
`make check` runs everything.
