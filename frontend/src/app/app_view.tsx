import { useRunnerStatus, useSignalValue } from "@tcn/state/react";
import { useCollection } from "../lib/domain/use_collection.js";
import { Status } from "@tcn/state/core";

import { useAppPresenter } from "./app_context.js";
import type { Route } from "./app_presenter.js";
import { SignInView } from "../features/auth/sign_in_view.js";
import { FamilyView } from "../features/families/family_view.js";
import { HeadToHeadView } from "../features/head_to_head/head_to_head_view.js";
import { MatchView } from "../features/match/match_view.js";
import { NewMatchView } from "../features/match/new_match_view.js";
import { ProfileView } from "../features/profile/profile_view.js";
import { ScoreboardView } from "../features/scoreboard/scoreboard_view.js";
import { Alert, Avatar, Empty } from "../components/ui.js";
import { UpdateBanner } from "./update_banner_view.js";

/**
 * The shell. Decides between signing in and the app itself, then routes to the
 * current screen.
 *
 * There is no third state any more. Signing in puts you on the one scoreboard,
 * so there is nothing to onboard into and no invitation to accept — the empty
 * case below is a fault, not a step.
 */
export function AppView() {
  const app = useAppPresenter();
  const authStatus = useSignalValue(app.session.statusBroadcast);
  const bootStatus = useRunnerStatus(app.bootBroadcast);
  const families = useCollection(app.families.families);
  const route = useSignalValue(app.routeBroadcast);
  const error = useSignalValue(app.errorBroadcast);

  if (authStatus === "loading" || bootStatus === Status.PENDING) {
    return <div className="async-loading">Loading…</div>;
  }

  if (authStatus === "signed_out") {
    return <SignInView session={app.session} />;
  }

  // Signing in joins the scoreboard, so being in no family means that failed.
  // Saying so beats an empty scoreboard that looks like nobody has played yet.
  if (families.length === 0) {
    return (
      <div className="app">
        <Empty emoji="🤔">You are not on the scoreboard yet. Try signing in again.</Empty>
        <button className="btn btn--ghost btn--block" onClick={() => void app.signOut()}>
          Sign out
        </button>
      </div>
    );
  }

  return (
    <div className="app">
      <UpdateBanner />

      <TopBar route={route} title="Gauntlet" />

      {error && <Alert message={error} />}

      {route.name === "scoreboard" && (
        <ScoreboardView presenter={app.scoreboard} app={app} />
      )}
      {route.name === "family" && <FamilyView presenter={app.families} app={app} />}
      {route.name === "new_match" && <NewMatchScreen />}
      {route.name === "match" && <MatchScreen />}
      {route.name === "player" && <PlayerScreen />}
      {route.name === "head_to_head" && <RivalryScreen />}

      {route.name === "scoreboard" && (
        <div className="bottombar">
          <button
            className="btn btn--primary btn--block btn--claim"
            onClick={() => app.navigate({ name: "new_match" })}
          >
            + Start a game
          </button>
        </div>
      )}
    </div>
  );
}

function TopBar({ route, title }: { route: Route; title: string }) {
  const app = useAppPresenter();
  const me = useSignalValue(app.session.userBroadcast);
  // Whatever the open screen calls itself — the game being played, the player
  // being looked at — falling back to the generic name for the route.
  const screenTitle = useSignalValue(app.screenTitleBroadcast);
  const atHome = route.name === "scoreboard";

  return (
    <header className="topbar">
      {!atHome && (
        <button className="btn btn--icon btn--ghost" onClick={app.goHome} aria-label="Back">
          ‹
        </button>
      )}
      <h1 className="topbar__title">
        {/* The eyebrow names the route above whatever the screen is showing —
            "Match" over "Catan". At home there is nothing to sit above: the
            title is the route, and printing it twice is just a stutter. */}
        {!atHome && <span className="topbar__sub">{subtitleFor(route)}</span>}
        {atHome ? title : screenTitle || titleFor(route)}
      </h1>
      {atHome && (
        <button
          className="btn btn--icon btn--ghost"
          onClick={() => app.navigate({ name: "family" })}
          aria-label="Family settings"
          style={{ padding: 4 }}
        >
          <Avatar user={me ?? undefined} size="sm" />
        </button>
      )}
    </header>
  );
}

function subtitleFor(route: Route): string {
  switch (route.name) {
    case "scoreboard":
      return ""; // never rendered — see TopBar
    case "new_match":
      return "New game";
    case "match":
      return "Match";
    case "player":
      return "Player";
    case "head_to_head":
      return "Head to head";
    case "family":
      return "Family";
  }
}

function titleFor(route: Route): string {
  switch (route.name) {
    case "new_match":
      return "What are we playing?";
    case "match":
      return "Match";
    case "player":
      return "The record";
    case "head_to_head":
      return "The rivalry";
    case "family":
      return "Family";
    default:
      return "Scoreboard";
  }
}

/*
 * Drill-in screens read their presenter off the root. Each is created on
 * navigation and disposed on the way out, so a null presenter simply means the
 * route has already been left.
 */

function MatchScreen() {
  const app = useAppPresenter();
  const presenter = useSignalValue(app.matchBroadcast);
  if (!presenter) return null;
  return <MatchView presenter={presenter} app={app} />;
}

function NewMatchScreen() {
  const app = useAppPresenter();
  const presenter = useSignalValue(app.newMatchBroadcast);
  if (!presenter) return null;
  return <NewMatchView presenter={presenter} />;
}

function PlayerScreen() {
  const app = useAppPresenter();
  const presenter = useSignalValue(app.profileBroadcast);
  if (!presenter) return null;
  return <ProfileView presenter={presenter} app={app} />;
}

function RivalryScreen() {
  const app = useAppPresenter();
  const presenter = useSignalValue(app.headToHeadBroadcast);
  if (!presenter) return null;
  return <HeadToHeadView presenter={presenter} app={app} />;
}
