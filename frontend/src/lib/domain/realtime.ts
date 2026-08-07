export type ChangeKind = "match" | "family" | "games";

export interface Change {
  kind: ChangeKind;
  matchId?: string;
}

type Listener = (change: Change) => void;

/**
 * The live connection to a family's changes.
 *
 * Server-Sent Events rather than WebSockets: every write in this app is already
 * an ordinary RPC, so nothing needs a client-to-server channel. That buys plain
 * HTTP — the session cookie and the dev proxy work untouched — and automatic
 * reconnection, which is the fiddly part of a socket.
 *
 * Events carry no data, only what changed. Reacting means dropping the matching
 * cache entry and re-reading through the normal API, so there is one code path
 * producing data and no way for a pushed copy to disagree with a fetched one.
 */
export class Realtime {
  private _source: EventSource | null = null;
  private _familyId = "";
  private readonly _listeners = new Set<Listener>();
  private _onReconnect: (() => void) | null = null;

  /** The family currently being watched, or "" when disconnected. */
  get familyId(): string {
    return this._familyId;
  }

  /**
   * Watches a family. Reconnecting to the same one is a no-op, so callers can
   * call this freely.
   *
   * onReconnect fires whenever a connection is (re-)established, including
   * after a drop. Anything could have changed while the connection was down, so
   * that is the moment to re-read rather than trust the cache.
   */
  connect(familyId: string, onReconnect?: () => void): void {
    if (!familyId) return;
    if (this._source && this._familyId === familyId) return;

    this.disconnect();
    this._familyId = familyId;
    if (onReconnect) this._onReconnect = onReconnect;

    const source = new EventSource(`/events?familyId=${encodeURIComponent(familyId)}`, {
      withCredentials: true,
    });
    this._source = source;

    source.addEventListener("open", () => {
      this._onReconnect?.();
    });

    source.addEventListener("change", (e) => {
      try {
        const change = JSON.parse((e as MessageEvent<string>).data) as Change;
        for (const listener of this._listeners) listener(change);
      } catch {
        // A malformed frame is not worth tearing the connection down for.
      }
    });

    // EventSource retries on its own, with backoff, so there is deliberately no
    // reconnect logic here. The server also closes each stream before Cloud
    // Run's request timeout, which shows up as a routine reconnect.
  }

  disconnect(): void {
    this._source?.close();
    this._source = null;
    this._familyId = "";
  }

  /** Subscribe to changes. Returns an unsubscribe function. */
  onChange(listener: Listener): () => void {
    this._listeners.add(listener);
    return () => this._listeners.delete(listener);
  }

  dispose(): void {
    this.disconnect();
    this._listeners.clear();
    this._onReconnect = null;
  }
}
