import { createClient, type Client } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { FamilyService } from "../gen/scoreboard/v1/family_pb.js";
import { GameService } from "../gen/scoreboard/v1/game_pb.js";
import { MatchService } from "../gen/scoreboard/v1/match_pb.js";
import { StatsService } from "../gen/scoreboard/v1/stats_pb.js";
import { UserService } from "../gen/scoreboard/v1/user_pb.js";

/** Every RPC client, grouped by service. Presenters reach the API through this. */
export interface Clients {
  users: Client<typeof UserService>;
  families: Client<typeof FamilyService>;
  games: Client<typeof GameService>;
  matches: Client<typeof MatchService>;
  stats: Client<typeof StatsService>;
}

/**
 * There is no auth interceptor here on purpose. The session is an HttpOnly
 * cookie, which the browser attaches to same-origin requests by itself — and
 * because it is HttpOnly, JavaScript could not read it to set a header even if
 * we wanted one. Production is same-origin because the Go binary serves this
 * bundle; development is same-origin because Vite proxies /api and /auth.
 */
export function createClients(): Clients {
  const transport = createConnectTransport({ baseUrl: "/api" });

  return {
    users: createClient(UserService, transport),
    families: createClient(FamilyService, transport),
    games: createClient(GameService, transport),
    matches: createClient(MatchService, transport),
    stats: createClient(StatsService, transport),
  };
}
