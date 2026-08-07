import { useEffect } from "react";
import { useSignalValue } from "@tcn/state/react";

import type { Collection } from "./collection.js";

/**
 * Binds a component to a store Collection.
 *
 * Use this rather than `useSignalValue(collection.broadcast)` for anything that
 * lives in the store. Reading the broadcast directly still renders the value,
 * but it subscribes to the underlying Runner and never registers with the
 * Collection itself — and `Collection.invalidate()` only refetches a cell that
 * something is watching. Bypass it and every `invalidateAll()` silently becomes
 * a no-op: the cell is marked stale and then nothing ever reloads it, so the UI
 * keeps showing what it fetched the first time until a full page refresh.
 *
 * Subscribing here also triggers an initial load when the cell is stale, so a
 * screen that mounts against cold cache fills itself in without every view
 * remembering to call `load()`.
 */
export function useCollection<T>(collection: Collection<T>): T {
  useEffect(() => collection.subscribe(() => {}), [collection]);
  return useSignalValue(collection.broadcast);
}
