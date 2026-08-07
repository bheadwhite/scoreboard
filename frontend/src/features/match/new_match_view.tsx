import { useEffect, useState } from "react";
import { useRunnerStatus, useSignalValue } from "@tcn/state/react";
import { useCollection } from "../../lib/domain/use_collection.js";
import { Status } from "@tcn/state/core";

import type { NewMatchPresenter } from "./new_match_presenter.js";
import { Alert, Avatar, Empty, Sheet } from "../../components/ui.js";
import { displayName, gameLabel } from "../../lib/format.js";

/** Pick a game, pick who is playing, open the lobby. */
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
                </span>
                <span style={{ color: selected ? "var(--gold)" : "var(--ink-faint)" }}>
                  {selected ? "✓" : "+"}
                </span>
              </button>
            );
          })}
        </div>
        <p style={{ margin: 0, fontSize: "0.82rem", color: "var(--ink-faint)" }}>
          They each decide whether to play. You can start once at least one person accepts,
          and anyone you miss can still join from the lobby.
        </p>
      </section>

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
                  : count > 1
                    ? `Invite ${count - 1} to ${gameLabel(game)}`
                    : `Set up ${gameLabel(game)}`}
        </button>
      </div>
    </div>
  );
}

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
  const [minPlayers, setMinPlayers] = useState(2);
  const [maxPlayers, setMaxPlayers] = useState(8);

  if (!open) return null;

  const valid = name.trim().length > 0 && minPlayers >= 2 && maxPlayers >= minPlayers;

  return (
    <Sheet title="Add a game" onDismiss={presenter.cancelAddGame}>
      <h2>Add a game</h2>
      <p style={{ color: "var(--ink-dim)", fontSize: "0.9rem" }}>
        It joins this family's list and stays there for next time.
      </p>

      <div className="stack">
        <div className="row" style={{ alignItems: "flex-end" }}>
          <div className="field" style={{ width: 84 }}>
            <label className="field__label" htmlFor="game-emoji">
              Icon
            </label>
            <input
              id="game-emoji"
              className="input"
              style={{ textAlign: "center", fontSize: "1.4rem" }}
              maxLength={4}
              placeholder="🎲"
              value={emoji}
              onChange={(e) => setEmoji(e.target.value)}
            />
          </div>
          <div className="field" style={{ flex: 1 }}>
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
              onChange={(e) => setMinPlayers(Number(e.target.value))}
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
              onChange={(e) => setMaxPlayers(Number(e.target.value))}
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
              minPlayers,
              maxPlayers,
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
