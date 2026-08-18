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

**The mechanic that defines the app: a win is a claim, and everybody else gets
asked.** Somebody names the winner, the claim sits publicly pending, and every
other player answers it. Only settled matches touch the scoreboard, and only
settled matches nobody is objecting to touch the *clean* scoreboard — which is
the number people actually argue about.

**Objecting marks a result; it does not block one.** Anybody who was not on the
named side can say "that is not what happened", at any point in the match's life
including years after it settled. The claim still settles on schedule. What the
objection does is make the match **contested**, and a contested match is held
out of every tally on the scoreboard — wins, losses, streaks, rivalries, game
kings — while staying completely visible as a match, with the objector's name
and reasons on it. It stops being contested the moment they take it back.

Nothing about any of this is a one-way door except settling itself. Every answer
is a *position held*, not an event logged: a player can move between conceding
and objecting whenever they like, and whoever made a claim — or anybody on the
side it named — can withdraw it outright. Silence eventually reads as agreement,
but the window is a week: a backstop for a match everybody walked away from, not
a countdown anyone is meant to race.

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

The one thing they cannot do is answer, so the protocol never asks them, and a
claim only they could have answered settles on the clock instead of on anybody's
word. Their agreement is deferred rather than assumed: when they sign up, a
service administrator links the two, the whole history becomes genuinely theirs,
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

**`Match.Answered()` needs somebody who *could* answer, not just an empty
queue.** `AwaitingConcession` skips placeholders, so a game whose only opponents
are placeholders has nobody outstanding from the first instant — and would
ratify immediately, recording a win on the say-so of the one person who claimed
it against players who by construction cannot contradict them. Hence the second
condition: `len(Answerable()) > 0`. Such a match waits out the concession window
and settles on the clock, which is silence read as agreement rather than
agreement invented from an empty list. Drop that condition and the app's central
rule has a hole in it exactly the size of one placeholder.

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

**An objection does not touch `concede_deadline`.** It used to clear it, to
separate disagreement from silence. Nothing turns on that difference now, and
leaving the clock alone is what guarantees every claim terminates: a match
everybody objected to settles exactly as promptly as one everybody accepted, and
lands contested. Clear the deadline again and you reintroduce the match nobody
can finish.

**`finalizeIfRatified` fires when every opponent has *answered*, not agreed.**
That one word is the difference. Waiting for unanimous concession made sense
when an objection put the match in front of a referee; with no referee, waiting
means waiting forever.

**`Concede` and `DisputeMatch` both accept a `final` match on purpose.** That is
how a result gets flagged after the fact — which is when most people first
notice one they disagree with, since they see it in the standings — and how the
flag comes off again. Narrow `answerable()` to `claimed` and the only remedy for
a bogus settled result becomes an administrator erasing the match.

**On a settled match the named winner may object too.** `answerable()` shuts
them out only while the claim is live, where they have the better door:
`WithdrawClaim` takes the whole thing off the table instead of leaving an
asterisk on a result that stands anyway. After it settles that door is closed,
and shutting them out as well would leave the person credited with the win as
the only player unable to say it did not happen — which is a position the
creator's short circuit can put somebody in, instantly, in a two-player game
where the creator's own claim is also the only answer it needs. So
`Match.Objectors()` scans every joined player while `AwaitingConcession()` still
only asks the opposing side: owing the claim an answer and being able to call it
wrong are different questions.

**Only the creator may name somebody else the winner.** Anyone may claim their
own side; `ClaimVictory` gates a different side on `m.CreatedBy`. When they do,
their claim is written as their own concession in the same transaction — they
are an opponent of the team they just crowned, and they have plainly already
accepted it — which is what lets `AwaitingConcession` and everything downstream
stay ignorant of who made the claim. `claimed_by_user_id` records who said it,
as distinct from `claimed_by_team_id` for who won.

**`WithdrawClaim` is open to the claim's author *and* the side it named.** The
second half is the counterweight to the first rule: being handed a win you did
not earn should be refusable outright, not only objectable to.

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
game played against real people, a claim the sweeper ratified while everybody
was away. A result somebody merely disagrees with is what objecting is for, and
objecting does not need anybody's permission.

**Finalization happens in two places** — `finalizeIfRatified` (last answer
lands) and `finalizeIfExpired` (clock ran out, checked lazily on read *and* by
the sweeper). Both funnel through `finalize()`, which is the only place a match
enters the permanent record, and finalizing is the *only* irreversible step in
the whole protocol. Note what it is not: it is not the moment a result becomes
uncontestable, because objections outlive it.

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
