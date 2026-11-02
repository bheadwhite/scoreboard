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

**The mechanic that defines the app: anybody can declare a win, and everybody
can say it did not happen.** Somebody names the winner and the result is on the
board immediately — there is no pending stage and no clock. Only settled matches
touch the scoreboard, and only settled matches nobody is objecting to touch the
*clean* scoreboard, which is the number people actually argue about.

**The protection is behind the result, not in front of it.** There was a
week-long concession window here and it is gone, because it protected nothing:
an unanswered claim settled exactly as claimed, and a unanimously rejected claim
settled exactly as claimed and contested. Both endings wrote the same row, so
the wait was a delay between somebody saying a thing and the app admitting they
had said it. What actually kept the record honest was the objection, and the
objection works just as well on a settled match — which is where people notice
wrong results anyway, since that is when they appear in the standings.

**Objecting marks a result; it does not block one.** Anybody in the match can
say "that is not what happened", at any point in its life including years later,
with nobody's permission and no majority needed. One objection is enough. What
it does is make the match **contested**, and a contested match is held out of
every tally on the scoreboard — wins, losses, streaks, rivalries, game kings —
while staying completely visible as a match, with the objector's name and
reasons on it. It stops being contested the moment they take it back. Since
nothing is pending any more, this is the *whole* of what stops somebody entering
a row of wins for themselves, which is why every path to it is kept as cheap and
as wide as it can be.

Nothing about any of this is a one-way door. Every answer is a *position held*,
not an event logged: a player can move between conceding and objecting whenever
they like. And the side a result credits can refuse it outright, which takes it
off the board and puts the match back in play — but only that side, never the
person who called it. **A win can always be refused; a loss can never be
erased.**

**Nobody adjudicates.** There is no dispute resolution, no referee and no
deadlock, because an objection cannot deadlock anything. The organizer can call
off a game that is still being played and a service administrator can erase a
match outright, but neither of them can decide who won, or clear somebody's
objection, or overrule a result. That is the whole argument of the app.

**Joining is the entire commitment.** There is no lobby and no ready check. A
match is posted with its game, its contract and its invitations, and it is live
from that moment; you confirm on your own device by joining, and the contract
was fixed before you tapped so there is nothing left to affirm. Somebody who
wanders up halfway through can still join, right until a winner is named.

**Players who have not signed up are still players.** Any member can add a
placeholder — an ordinary `users` row nobody can log in to — so a game somebody
was actually in gets recorded with everybody in it rather than entered short or
not at all. They join matches, hold a team, win, lose, and appear in the
standings and in other people's head-to-head records, all without a single query
knowing they are different.

The one thing they cannot do is answer, so nothing ever waits on them — and a
result whose losing side is *entirely* placeholders is marked **unwitnessed**,
because nobody in it was in a position to contradict it. It counts like any
other; the mark is the app saying out loud that the check every other result is
subject to was unavailable here. Their agreement is deferred rather than
assumed: when they sign up, a service administrator links the two, the whole history becomes genuinely theirs,
and because objections outlive finalization they can go back and flag anything
in it. That deferred objection is the entire justification for one player being
able to enter another person into a game without asking.

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
`Rivalries()` pass over the same *clean* finalized participations, so adding
another question of the form "who have I not beaten lately" costs a function,
not a query — and gets the contested filtering for free by construction.

**A placeholder is a `users` row with `provider = 'placeholder'`.** That is the
whole mechanism, and it is deliberate: everything keys on `users.id`, so
modelling them separately would have meant teaching standings, head-to-head,
profiles, participations and every mapper about a second kind of player. The
provider identity is exactly what they lack, so it is what says so. `provider_id`
is a fresh uuid rather than empty because `users_provider_identity_idx` is a
plain unique index on `(provider, provider_id)` — it has to be, since the sign-in
upsert names it as an `ON CONFLICT` target — so two placeholders sharing a blank
id would collide with each other. `isPlaceholder` in `store.go` is the one SQL
definition; `model.User.Placeholder` is the Go one, and every query that loads a
player selects it.

**`invitePlayer` joins a placeholder instead of inviting them.** An invitation
is a question, and there is no device to put it on — an invitation to a
placeholder would sit unanswered forever while the person plainly sitting at the
table never appeared in the game. The branch lives in that one function so
`CreateMatch` and `InvitePlayers` cannot disagree about it.

**`Match.Unwitnessed()` is what is left of a rule that used to be enforced by
delay.** `Match.Answered()` required at least one opponent who could actually
speak before a claim could ratify, so a game whose only losers were placeholders
could not settle early and sat out the week instead. The intent was right and
the mechanism did nothing: at the end of the week it settled exactly as claimed
anyway, against players who by construction cannot contradict anybody. Seven
days of silence from people with no way to make a sound is not agreement, and
reporting it as a wait was worse than saying nothing. So the app says so on the
result instead — it counts, and it carries the one fact that makes it different.
Do not turn this back into a refusal to record: the honest options are marking
it or forbidding placeholder-only matches outright, and the second one bans real
games.

**Linking is admin-only and irreversible.** `UserService.LinkPlaceholder` is
gated on `ADMIN_EMAILS`, the same as `VoidMatch` and for the same reason — both
rewrite the permanent record, and a family organizer being able to move wins
between the people they play against is the shape of thing family roles are kept
away from. The account's id survives, never the placeholder's: the account id is
carried in a session cookie, so moving it would sign somebody out of their own
history. The rewrite has to reach every reference, not just participations —
`matches.created_by`, `matches.claimed_by_user_id`, `games.created_by`,
`families.owner_id`, `family_members` — because the placeholder row is deleted
at the end and anything missed is a dangling foreign key. It refuses outright
when both players are in the same match: whichever row were dropped would be a
guess about something the database cannot settle, and the possibility worth
stopping for is that the placeholder is not who the administrator thinks it is.

**Every participant always has a team**, including in free-for-all games, where
each player gets a team of one. This collapses "concede to a player" and
"concede to a team" into one code path. Do not add a parallel non-team path.

**Contested is derived, never stored.** `Match.Contested()` asks the answers —
does any joined player off the winning side have `disputed_at` set — and that is
true of a claim mid-flight and of a match that settled in 2019, because the
participant row outlives finalization. Store a flag instead and you inherit the
job of maintaining it across concede, dispute, withdraw, re-claim and finalize,
and the day one of those forgets, a disputed result is silently back on the
scoreboard. The same question is asked once in SQL — the `EXISTS` in
`LoadParticipations` — and those two have to be read together, including the
team test: a stray `disputed_at` on somebody on the winning side, left by a
claim that was withdrawn and remade, is not an objection to this result.

**There is no clock and no pending stage. Do not add one back.** `ClaimVictory`
calls `finalize()` in the same transaction. The `claimed` status, `Answered()`,
`AwaitingConcession()`, `finalizeIfRatified`, `finalizeIfExpired`,
`SweepExpiredClaims` and `CONCEDE_WINDOW` are all gone, and `concede_deadline`
survives only as an unread column (see below). If a "let people confirm first"
requirement turns up again, the thing to re-read is why it went: every ending
that stage could reach wrote the identical row, so it delayed the record without
filtering it. A confirmation step that cannot change the outcome is a lobby with
a countdown on it.

**`concede_deadline` is still in the schema and nothing reads it.** Same reason
`ready_at` is still there: dropping a column in the release that stops reading
it takes the rollback with it, and the previous binary selects it on every match
read. `0009` nulls it and drops `matches_sweep_idx`. Drop both columns in a
later migration, once nobody would roll back that far.

**`Concede` and `DisputeMatch` only ever see a `final` match now.** That is not
a narrowing, it is the only state a match with a result can be in. Objecting
after the fact is when most people first notice a result they disagree with,
since they see it in the standings — and it is now the *only* check on the
record, so `answerable()` is deliberately as permissive as it can be: in the
match, and there is a result.

**On a settled result the named winner may object too.** `answerable()` lets
everybody in the match answer, including the side credited with the win.
Shutting them out would leave the person a result names as the only player
unable to say it did not happen — a position the creator's short circuit can put
somebody in instantly, since whoever posts a match can name any side and the
result lands before the supposed winner has looked at their phone. So
`Match.Objectors()` scans every joined player, while `Answerable()` still only
names the losing side: being the person a result was recorded against and being
able to call it wrong are different questions.

**Only the creator may name somebody else the winner.** Anyone may claim their
own side; `ClaimVictory` gates a different side on `m.CreatedBy`. When they do,
their claim is written as their own concession in the same transaction — they
are an opponent of the team they just crowned, and they have plainly already
accepted it — which puts their position on the answer board where everybody can
see whose word the result rests on. `claimed_by_user_id` records who said it, as
distinct from `claimed_by_team_id` for who won.

**`WithdrawClaim` belongs to the side the result names, and to nobody else.**
Not to whoever called it, which is the one rule here that cannot bend. Whoever
posts a match can name any side, so an author who could retract would be able to
credit their opponent, take the loss, and then take the loss back off the board
— and a record you can retract a defeat from is not a record. Hence: **a win can
always be refused; a loss can never be erased.** It reaches a `final` match,
which is what makes finalizing no longer irreversible, and that is the price of
having no pending stage to catch a misplaced tap in. Somebody who called a game
wrong and is not on the winning side objects to it instead, or asks an
administrator to void it.

**Voiding is the only exit from the permanent record, and it is a hard delete.**
`MatchService.VoidMatch` erases the row; participants and teams go with it. That
is deliberate — a voided match kept as a row would be another status every stats
query has to remember to skip. Because stats are computed on read, deleting the
row *is* the whole cleanup: standings, head-to-head and profiles simply stop
counting it. It is gated on `ADMIN_EMAILS` (a service administrator, checked
against `users.email`), **not** on any family role, and unlike every other match
handler it does not require family membership — running the deployment is the
authorization. Organizers get `CancelMatch` on a game other people are in, and
that is all — nothing anywhere lets one player decide a result, and voiding is
not a way around that. It is for a match that should never have existed: a test
game played against real people, a result somebody entered against players who
were never there. A result somebody merely disagrees with is what objecting is for, and
objecting does not need anybody's permission.

**Finalization happens in one place** — `finalize()`, called by `ClaimVictory`
and nothing else. It is the only place a match enters the permanent record. Note
what it is *not*: it is not the moment a result becomes uncontestable, because
objections outlive it; and it is not irreversible either, because the side a
result credits can hand it back. What it is is the moment a result starts
counting.

**`POST /tasks/sweep` no longer sweeps anything.** It pings the database, which
keeps a free-tier Supabase project from being paused for inactivity. Cloud
Scheduler hits it every 15 minutes. The name, path and token are kept so the
existing scheduler job keeps working; nothing anywhere depends on it being
called for correctness any more.

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
logic unit-testable without a database. It is also what makes a retractable
objection free: a result moves between the clean numbers and the contested ones
with nothing to recompute and nothing to invalidate.

**The contested split happens exactly once, in `stats.Split`.** Every exported
function in the package calls it at the top and everything below works on an
already-filtered slice. Do not thread a "skip if contested" test through the
individual functions — that is precisely how `HeadToHead` and `Rivalries` would
drift apart about which meetings exist, and they have to agree for the same
reason they already have to agree about free-for-all losses. Contested tallies
are counted by running the same code over the other half.

## Testing

Go tests live beside the code (`go test ./...`); the stats package carries the
real coverage. Frontend uses vitest + jsdom, configured inline in
`vite.config.ts`, with tests as `*.test.ts(x)` next to their source.
`make check` runs everything.
