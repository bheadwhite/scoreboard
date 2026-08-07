import { describe, expect, it, vi } from "vitest";

import { Collection } from "./collection.js";

const tick = () => new Promise((r) => setTimeout(r, 0));

describe("Collection", () => {
  it("serves the cached value without refetching", async () => {
    const fetch = vi.fn(async () => "first");
    const cell = new Collection<string>("", fetch);

    expect(await cell.load()).toBe("first");
    expect(await cell.load()).toBe("first");
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("refetches once invalidated", async () => {
    let value = "first";
    const fetch = vi.fn(async () => value);
    const cell = new Collection<string>("", fetch);

    await cell.load();
    value = "second";
    cell.invalidate();

    expect(await cell.load()).toBe("second");
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  /**
   * The regression that broke joining a family: views read
   * `collection.broadcast` directly, so the Collection had no subscribers and
   * `invalidate()` silently never reloaded. Data stayed stale until a full page
   * refresh. `useCollection` exists to keep this registration in place.
   */
  it("reloads a subscribed cell immediately on invalidate", async () => {
    let value = "first";
    const fetch = vi.fn(async () => value);
    const cell = new Collection<string>("", fetch);

    await cell.load();
    const unsubscribe = cell.subscribe(() => {});
    await tick();

    value = "second";
    cell.invalidate();
    await tick();

    // No explicit load() — a watched cell must refresh itself.
    expect(cell.peek()).toBe("second");
    unsubscribe();
  });

  it("does not eagerly refetch a cell nobody is watching", async () => {
    const fetch = vi.fn(async () => "x");
    const cell = new Collection<string>("", fetch);

    await cell.load();
    cell.invalidate();
    await tick();

    // Still one call: an unwatched cell waits until something asks for it.
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("coalesces concurrent loads into one request", async () => {
    const fetch = vi.fn(async () => {
      await tick();
      return "x";
    });
    const cell = new Collection<string>("", fetch);

    await Promise.all([cell.load(), cell.load(), cell.load()]);
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("loads a stale cell as soon as something subscribes", async () => {
    const fetch = vi.fn(async () => "loaded");
    const cell = new Collection<string>("", fetch);

    const unsubscribe = cell.subscribe(() => {});
    await tick();

    expect(cell.peek()).toBe("loaded");
    unsubscribe();
  });

  it("set() writes through without a request", async () => {
    const fetch = vi.fn(async () => "fetched");
    const cell = new Collection<string>("", fetch);

    cell.set("written");
    expect(await cell.load()).toBe("written");
    expect(fetch).not.toHaveBeenCalled();
  });
});
