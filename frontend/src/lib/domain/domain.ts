import { createClients, type Clients } from "../clients.js";
import { Session } from "./session.js";
import { Store } from "./store.js";

/**
 * Domain is the app's headless core: the API clients, the cache over them, and
 * the session. It has no React and no view state — it is the single object
 * every presenter is given a hard reference to.
 *
 *   presenter → domain.store.<collection>   cached, shared, invalidatable reads
 *   presenter → domain.api.<service>.<rpc>  writes, and one-off reads
 *   presenter → domain.session              who is signed in
 */
export interface Domain {
  readonly api: Clients;
  readonly store: Store;
  readonly session: Session;
}

export function createDomain(api: Clients = createClients()): Domain {
  return { api, store: new Store(api), session: new Session() };
}
