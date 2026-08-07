/**
 * KeyPromiseCache coalesces concurrent calls for the same key onto one promise.
 *
 * Without it, three components mounting at once means three identical requests.
 * With it, the first call starts the work and the other two await the same
 * promise. The entry is evicted when the promise settles, so only overlapping
 * duplicates share it — this is a de-duplicator, not a result cache. Caching
 * results is Collection's job.
 */
export class KeyPromiseCache<T> {
  private _inflight = new Map<string, Promise<T>>();
  private _fetch: (key: string) => Promise<T>;

  constructor(fetch: (key: string) => Promise<T>) {
    this._fetch = fetch;
  }

  get(key = "[_KEY_DEFAULT_]"): Promise<T> {
    const existing = this._inflight.get(key);
    if (existing != null) return existing;

    const promise = this._fetch(key).finally(() => {
      this._inflight.delete(key);
    });
    this._inflight.set(key, promise);
    return promise;
  }

  remove(key: string): void {
    this._inflight.delete(key);
  }

  empty(): void {
    this._inflight.clear();
  }
}
