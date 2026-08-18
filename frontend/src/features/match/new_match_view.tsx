import { useEffect, useState } from "react";
import { useRunnerStatus, useSignalValue } from "@tcn/state/react";
import { useCollection } from "../../lib/domain/use_collection.js";
import { Status } from "@tcn/state/core";

import type { NewMatchPresenter } from "./new_match_presenter.js";
import { ContractKind } from "../../gen/scoreboard/v1/common_pb.js";
import { Alert, Avatar, Empty, Sheet } from "../../components/ui.js";
import { displayName, gameLabel } from "../../lib/format.js";

/**
 * Pick a game, say how it is won, pick who to ask, post it.
 *
 * The contract lives on this screen rather than the match screen because the
 * match is live the moment it is posted and joining it is the whole commitment.
 * Everything anybody is agreeing to has to be visible before they tap, and
 * nothing here can be edited afterward.
 */
export function NewMatchView({ presenter }: { presenter: NewMatchPresenter }) {
  const gameId = useSignalValue(presenter.gameIdBroadcast);
  const invited = useSignalValue(presenter.invitedBroadcast);
  const games = useCollection(presenter.games);
  const members = useCollection(presenter.members);
  const error = useSignalValue(presenter.errorBroadcast);
  const status = useRunnerStatus(presenter.createBroadcast);
  const busy = status === Status.PENDING;

  useEffect(() => {
    void presenter.load();
  }, [presenter]);

  const game = presenter.selectedGame(games);
  const count = presenter.playerCount();
  const ready = presenter.canCreate(games);

  return (
    <div className="stack stack--lg">
      <section className="stack">
        <span className="section-label">What are you playing?</span>
        <div className="card card--flush">
          {games.length === 0 ? (
            <Empty emoji="🎲">No games yet.</Empty>
          ) : (
            games.map((g) => (
              <button
                key={g.id}
                className="rowbtn"
                aria-pressed={gameId === g.id}
                onClick={() => presenter.selectGame(g.id)}
                style={gameId === g.id ? { background: "var(--chalk)" } : undefined}
              >
                <span style={{ fontSize: "1.4rem", width: 30, textAlign: "center" }}>
                  {g.emoji || "🎲"}
                </span>
                <span className="rowbtn__main">
                  <span className="rowbtn__name">{g.name}</span>
                  <span className="rowbtn__meta num">
                    {g.minPlayers}–{g.maxPlayers} players
                    {g.teamBased ? " · teams" : ""}
                  </span>
                </span>
                {gameId === g.id && <span style={{ color: "var(--gold)" }}>✓</span>}
              </button>
            ))
          )}
          <button
            className="rowbtn"
            onClick={presenter.openAddGame}
            style={{ color: "var(--gold)", fontWeight: 650 }}
          >
            <span style={{ fontSize: "1.4rem", width: 30, textAlign: "center" }}>＋</span>
            <span className="rowbtn__main">
              <span className="rowbtn__name" style={{ color: "var(--gold)" }}>
                Add a game
              </span>
              <span className="rowbtn__meta">Something your family plays that is not listed</span>
            </span>
          </button>
        </div>
      </section>

      <AddGameSheet presenter={presenter} />

      <ContractSection presenter={presenter} />

      <section className="stack">
        <div className="row row--between">
          <span className="section-label">Who do you want to invite?</span>
          <span className="section-label num">
            {count}
            {game ? ` / ${game.maxPlayers}` : ""}
          </span>
        </div>
        <div className="card card--flush">
          {members.map((m) => {
            const id = m.user?.id ?? "";
            const isMe = id === presenter.meId;
            const selected = isMe || invited.has(id);
            return (
              <button
                key={id}
                className="rowbtn"
                aria-pressed={selected}
                onClick={() => !isMe && presenter.toggleInvite(id)}
                disabled={isMe}
              >
                <Avatar user={m.user} size="sm" />
                <span className="rowbtn__main">
                  <span className="rowbtn__name">
                    {displayName(m.user)}
                    {isMe && <span style={{ color: "var(--ink-faint)" }}> (you)</span>}
                  </span>
                  {/* Picking them puts them straight in rather than asking, so
                      the row says so before it is tapped. */}
                  {m.user?.placeholder && (
                    <span className="rowbtn__meta">not on the app · added, not asked</span>
                  )}
                </span>
                <span style={{ color: selected ? "var(--gold)" : "var(--ink-faint)" }}>
                  {selected ? "✓" : "+"}
                </span>
              </button>
            );
          })}
          <button
            className="rowbtn"
            onClick={presenter.openAddPlayer}
            style={{ color: "var(--gold)", fontWeight: 650 }}
          >
            <span style={{ fontSize: "1.4rem", width: 30, textAlign: "center" }}>＋</span>
            <span className="rowbtn__main">
              <span className="rowbtn__name" style={{ color: "var(--gold)" }}>
                Add someone who is not on the app
              </span>
              <span className="rowbtn__meta">
                So a game they were in gets recorded with everybody in it
              </span>
            </span>
          </button>
        </div>
        <p style={{ margin: 0, fontSize: "0.82rem", color: "var(--ink-faint)" }}>
          They each decide whether to play, and joining is all there is to it — whoever taps yes is
          in the game. Anyone you miss can still join later, right up until somebody calls the win.
        </p>
      </section>

      <AddPlayerSheet presenter={presenter} />

      {error && <Alert message={error} />}

      <div className="bottombar">
        <button
          className="btn btn--primary btn--block btn--claim"
          disabled={busy || !ready}
          onClick={() => void presenter.create()}
        >
          {busy
            ? "Setting up…"
            : !game
              ? "Pick a game"
              : count > game.maxPlayers
                ? `Too many for ${game.name}`
                : !ready
                  ? "Say how it is won"
                  : count > 1
                    ? `Start ${gameLabel(game)} and ask ${count - 1}`
                    : `Start ${gameLabel(game)}`}
        </button>
      </div>
    </div>
  );
}

/**
 * Adding somebody who has not signed up.
 *
 * Everything about this sheet is trying to be honest about what it does, because
 * what it does is unusual for this app: it puts a person into a game without
 * asking them. That is unavoidable — there is no device to ask — and the whole
 * justification is on the other end, so the copy says it. They get a real place
 * on the scoreboard, they are not asked to agree to anything, and when they do
 * sign up they inherit the lot and can argue with any of it.
 */
function AddPlayerSheet({ presenter }: { presenter: NewMatchPresenter }) {
  const open = useSignalValue(presenter.addingPlayerBroadcast);
  const status = useRunnerStatus(presenter.addPlayerBroadcast);
  const busy = status === Status.PENDING;
  const [name, setName] = useState("");

  if (!open) return null;

  const submit = () => {
    const trimmed = name.trim();
    if (!trimmed) return;
    void presenter.addPlaceholder(trimmed);
    setName("");
  };

  return (
    <Sheet title="Add a player" onDismiss={presenter.cancelAddPlayer}>
      <h2>Who is playing?</h2>
      <p style={{ color: "var(--ink-dim)", fontSize: "0.9rem" }}>
        For somebody at the table who has not signed up. They get a real spot on the scoreboard —
        wins, losses, head-to-heads, the lot — so the game gets recorded with everybody who was
        actually in it.
      </p>
      <div className="stack">
        <div className="field">
          <label className="field__label" htmlFor="placeholder-name">
            Their name
          </label>
          <input
            id="placeholder-name"
            className="input"
            placeholder="Grandma"
            value={name}
            autoFocus
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && submit()}
          />
        </div>
        <p style={{ margin: 0, fontSize: "0.82rem", color: "var(--ink-faint)" }}>
          They cannot concede or object to anything, because there is nothing to do it from — so
          they are added to the game rather than asked, and a result only they could have answered
          waits out the full week instead of counting straight away. When they sign up, an
          administrator can join the two together and they get to argue with every result they
          inherit.
        </p>
        <button
          className="btn btn--primary btn--block"
          disabled={busy || name.trim() === ""}
          onClick={submit}
        >
          Add {name.trim() || "them"}
        </button>
        <button className="btn btn--ghost btn--block" onClick={presenter.cancelAddPlayer}>
          Never mind
        </button>
      </div>
    </Sheet>
  );
}

const CONTRACT_OPTIONS = [
  { kind: ContractKind.SINGLE_GAME, label: "Single game", target: 0 },
  { kind: ContractKind.FIRST_TO_SCORE, label: "First to…", target: 500 },
  { kind: ContractKind.BEST_OF, label: "Best of…", target: 3 },
  { kind: ContractKind.LAST_STANDING, label: "Last standing", target: 0 },
  { kind: ContractKind.CUSTOM, label: "Custom", target: 0 },
];

/**
 * How this one is won, declared up front and fixed from then on.
 *
 * It used to be set on the match screen, collaboratively, with everyone
 * re-confirming whenever it changed — which is what the ready check existed to
 * do. Moving it here removes that whole round trip: there is nothing to
 * re-confirm because there is nothing left to change, and joining a match with
 * its terms already on the screen is a stronger statement of agreement than
 * affirming terms somebody could still edit.
 */
function ContractSection({ presenter }: { presenter: NewMatchPresenter }) {
  const kind = useSignalValue(presenter.contractKindBroadcast);
  const target = useSignalValue(presenter.contractTargetBroadcast);
  const detail = useSignalValue(presenter.contractDetailBroadcast);

  const needsTarget = kind === ContractKind.FIRST_TO_SCORE || kind === ContractKind.BEST_OF;
  const needsDetail = kind === ContractKind.CUSTOM;

  return (
    <section className="stack">
      <span className="section-label">How is it won?</span>
      <div className="chips" role="group" aria-label="Victory condition">
        {CONTRACT_OPTIONS.map((o) => (
          <button
            key={o.kind}
            className="chip"
            aria-pressed={kind === o.kind}
            onClick={() => presenter.selectContract(o.kind, o.target)}
          >
            {o.label}
          </button>
        ))}
      </div>

      {needsTarget && (
        <div className="field">
          <label className="field__label" htmlFor="contract-target">
            {kind === ContractKind.BEST_OF ? "How many rounds" : "Points to win"}
          </label>
          <input
            id="contract-target"
            className="input num"
            type="number"
            inputMode="numeric"
            min={1}
            value={target}
            onChange={(e) => presenter.setContractTarget(e.target.value)}
            onFocus={(e) => e.target.select()}
          />
        </div>
      )}

      <div className="field">
        <label className="field__label" htmlFor="contract-detail">
          {needsDetail ? "How is this won?" : "Notes (optional)"}
        </label>
        <input
          id="contract-detail"
          className="input"
          placeholder={needsDetail ? "Whoever still has chips at midnight" : "House rules, etc."}
          value={detail}
          onChange={(e) => presenter.setContractDetail(e.target.value)}
        />
      </div>

      <p style={{ margin: 0, fontSize: "0.82rem", color: "var(--ink-faint)" }}>
        Fixed once you start. Everyone who joins is agreeing to it, which is why nobody has to
        confirm anything afterward.
      </p>
    </section>
  );
}

/**
 * Icons families are actually likely to want, arranged roughly by kind: cards,
 * board and tile games, then things played standing up. A grid rather than a
 * text field because typing an emoji means hunting for the system picker on a
 * desktop, and this is the one purely decorative field in the app.
 */
const GAME_EMOJI = [
  "🎲", "🃏", "♠️", "♥️", "♦️", "♣️", "🀄", "🎴",
  "♟️", "🧩", "🔤", "🎩", "🚂", "🌾", "🏰", "🗺️",
  "🎯", "🎱", "🎳", "🏓", "🏸", "🥍", "⛳", "🥏",
  "🏀", "⚽", "🏈", "⚾", "🎾", "🏐", "🏒", "🥊",
  "🌽", "🥒", "🪁", "🛶", "🎮", "🕹️", "🎪", "🏆",
];

/**
 * Adds a game to this family's own list. The built-in catalog is a starting
 * point, not a constraint — every family plays something nobody seeded.
 */
function AddGameSheet({ presenter }: { presenter: NewMatchPresenter }) {
  const open = useSignalValue(presenter.addingGameBroadcast);
  const status = useRunnerStatus(presenter.addGameBroadcast);
  const busy = status === Status.PENDING;

  const [name, setName] = useState("");
  const [emoji, setEmoji] = useState("");
  const [teamBased, setTeamBased] = useState(false);
  // Text rather than numbers, so the field can actually be emptied — see the
  // note in ContractPicker.
  const [minPlayers, setMinPlayers] = useState("2");
  const [maxPlayers, setMaxPlayers] = useState("8");
  const min = Number(minPlayers);
  const max = Number(maxPlayers);

  if (!open) return null;

  const valid =
    name.trim().length > 0 &&
    Number.isFinite(min) &&
    Number.isFinite(max) &&
    min >= 2 &&
    max >= min;

  return (
    <Sheet title="Add a game" onDismiss={presenter.cancelAddGame}>
      <h2>Add a game</h2>
      <p style={{ color: "var(--ink-dim)", fontSize: "0.9rem" }}>
        It joins this family's list and stays there for next time.
      </p>

      <div className="stack">
        <div className="field">
          <label className="field__label" htmlFor="game-name">
            Name
          </label>
          <input
            id="game-name"
            className="input"
            placeholder="Nertz"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>

        <div className="field">
          <span className="field__label">Icon {emoji && <span aria-hidden="true">{emoji}</span>}</span>
          <div className="emoji-grid" role="group" aria-label="Pick an icon">
            {GAME_EMOJI.map((choice) => (
              <button
                key={choice}
                type="button"
                className="emoji-grid__item"
                aria-label={`Icon ${choice}`}
                aria-pressed={emoji === choice}
                onClick={() => setEmoji(emoji === choice ? "" : choice)}
              >
                {choice}
              </button>
            ))}
          </div>
          <p style={{ margin: 0, fontSize: "0.78rem", color: "var(--ink-faint)" }}>
            Optional — a game with no icon gets 🎲. Tap again to clear.
          </p>
        </div>

        <div className="row">
          <div className="field" style={{ flex: 1 }}>
            <label className="field__label" htmlFor="game-min">
              Fewest players
            </label>
            <input
              id="game-min"
              className="input num"
              type="number"
              inputMode="numeric"
              min={2}
              value={minPlayers}
              onChange={(e) => setMinPlayers(e.target.value)}
              onFocus={(e) => e.target.select()}
            />
          </div>
          <div className="field" style={{ flex: 1 }}>
            <label className="field__label" htmlFor="game-max">
              Most players
            </label>
            <input
              id="game-max"
              className="input num"
              type="number"
              inputMode="numeric"
              min={2}
              value={maxPlayers}
              onChange={(e) => setMaxPlayers(e.target.value)}
              onFocus={(e) => e.target.select()}
            />
          </div>
        </div>

        <button
          className="chip"
          style={{ alignSelf: "flex-start" }}
          aria-pressed={teamBased}
          onClick={() => setTeamBased((v: boolean) => !v)}
        >
          {teamBased ? "👥 Played in teams" : "🙋 Every player for themselves"}
        </button>

        <button
          className="btn btn--primary btn--block"
          disabled={busy || !valid}
          onClick={() =>
            void presenter.addGame({
              name: name.trim(),
              emoji: emoji.trim(),
              teamBased,
              minPlayers: min,
              maxPlayers: max,
            })
          }
        >
          {busy ? "Adding…" : "Add game"}
        </button>
        <button className="btn btn--ghost btn--block" onClick={presenter.cancelAddGame}>
          Cancel
        </button>
      </div>
    </Sheet>
  );
}
