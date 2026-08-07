import { Collection } from "./collection.js";

/**
 * A family of Collections keyed by a query parameter — standings per
 * (family, game, range), head-to-head per pair of players, and so on. Cells are
 * created lazily on first access and share the parent's invalidation, so
 * finalizing one match refreshes every scoreboard that was watching.
 */
export class KeyedCollection<T> {
  private readonly _cells = new Map<string, Collection<T>>();
  private readonly _initial: T;
  private readonly _fetch: (key: string) => Promise<T>;

  constructor(initial: T, fetch: (key: string) => Promise<T>) {
    this._initial = initial;
    this._fetch = fetch;
  }

  /** The cell for a key, created on first use. */
  for(key: string): Collection<T> {
    let cell = this._cells.get(key);
    if (cell == null) {
      cell = new Collection<T>(this._initial, () => this._fetch(key));
      this._cells.set(key, cell);
    }
    return cell;
  }

  invalidate(key: string): void {
    this._cells.get(key)?.invalidate();
  }

  invalidateAll(): void {
    for (const cell of this._cells.values()) cell.invalidate();
  }

  dispose(): void {
    for (const cell of this._cells.values()) cell.dispose();
    this._cells.clear();
  }
}
