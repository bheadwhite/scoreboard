import { Runner } from "@tcn/state/core";
import type { IRunnerBroadcast } from "@tcn/state/core";

import { KeyPromiseCache } from "../key_promise_cache.js";

/**
 * A Collection is one piece of server-owned data held in the store: a cache
 * cell with a fetch, a staleness flag, and subscribers.
 *
 * It does three things at once:
 *  - **Coalescing.** Concurrent reads while a fetch is in flight share the one
 *    request.
 *  - **Caching.** A fresh collection answers `load()` from memory. It only goes
 *    back to the server when marked stale.
 *  - **Reactivity.** It holds a Runner, so `broadcast` drops straight into
 *    `useSignalValue` and `AsyncBoundary` without any adapter.
 */
export class Collection<T> {
  private readonly _runner: Runner<T>;
  private readonly _inflight: KeyPromiseCache<T>;
  private readonly _subscribers = new Set<() => void>();
  private _stale = true;

  constructor(initial: T, fetch: () => Promise<T>) {
    this._runner = new Runner<T>(initial);
    this._inflight = new KeyPromiseCache<T>(() =>
      this._runner.execute(async () => {
        const value = await fetch();
        this._stale = false;
        return value;
      }),
    );
  }

  /** Runner broadcast for views, `useSignalValue`, and `AsyncBoundary`. */
  get broadcast(): IRunnerBroadcast<T> {
    return this._runner.broadcast;
  }

  /** The current cached value — the initial placeholder until first load. */
  peek(): T {
    return this._runner.get();
  }

  load(force = false): Promise<T> {
    if (!force && !this._stale) return Promise.resolve(this._runner.get());
    return this._inflight.get();
  }

  /**
   * Write a value straight into the cell without a request. Used when a
   * mutation already returned the fresh entity, so the UI updates without a
   * round trip.
   */
  set(value: T): void {
    this._stale = false;
    this._runner.set(value);
  }

  /** Edit the cached value in place. */
  transform(fn: (value: T) => T): void {
    this.set(fn(this._runner.get()));
  }

  /**
   * Mark stale. A cell somebody is watching refetches immediately; an unwatched
   * one waits until something asks for it.
   */
  invalidate(): void {
    this._stale = true;
    if (this._subscribers.size > 0) {
      void this.load(true).catch(() => {});
    }
  }

  /**
   * Alias for `invalidate`, so a Collection and a KeyedCollection present the
   * same surface to the store's bulk refresh.
   */
  invalidateAll(): void {
    this.invalidate();
  }

  subscribe(listener: () => void): () => void {
    this._subscribers.add(listener);
    const sub = this._runner.broadcast.subscribe(() => listener());
    if (this._stale) void this.load().catch(() => {});
    return () => {
      this._subscribers.delete(listener);
      sub.unsubscribe();
    };
  }

  dispose(): void {
    this._subscribers.clear();
    this._runner.dispose();
  }
}
