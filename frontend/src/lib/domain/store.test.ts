import { describe, expect, it, vi } from "vitest";

import type { Clients } from "../clients.js";
import type { Match } from "../../gen/scoreboard/v1/common_pb.js";
import { key, Store } from "./store.js";

function storeWith(total: number) {
  const listMatches = vi.fn(async ({ limit, offset }: { limit: number; offset: number }) => ({
    matches: Array.from({ length: Math.max(0, Math.min(limit, total - offset)) }, () => ({}) as Match),
  }));
  const api = { matches: { listMatches } } as unknown as Clients;
  return { store: new Store(api), listMatches };
}

describe("Store.playerMatches", () => {
  it("makes one request for a history that fits in a page", async () => {
    const { store, listMatches } = storeWith(31);
    const cell = store.playerMatches.for(key.playerMatches("f", "u"));
    expect(await cell.load()).toHaveLength(31);
    expect(listMatches).toHaveBeenCalledTimes(1);
  });

  it("keeps asking until it has every game", async () => {
    const { store, listMatches } = storeWith(450);
    const cell = store.playerMatches.for(key.playerMatches("f", "u"));
    expect(await cell.load()).toHaveLength(450);
    expect(listMatches).toHaveBeenCalledTimes(3);
  });
});
