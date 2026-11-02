import type { Clients } from "../clients.js";
import type { Family, FamilyMember, Game, Match } from "../../gen/scoreboard/v1/common_pb.js";
import { MatchStatus } from "../../gen/scoreboard/v1/common_pb.js";
import type {
  GetHeadToHeadResponse,
  GetPlayerProfileResponse,
  GetStandingsResponse,
  StatsRange,
} from "../../gen/scoreboard/v1/stats_pb.js";
import { Collection } from "./collection.js";
import { KeyedCollection } from "./keyed_collection.js";

/**
 * The statuses that mean "a game is happening right now" — one, now that a
 * match either has a result or does not.
 *
 * Three shorter than it once was, and none of the losses is a narrowing. There
 * is no lobby, because a match is in play from the moment it is posted. There
 * is no disputed, because an objection marks a match rather than moving it. And
 * there is no claimed: naming a winner used to open a pending stage that
 * collected answers for a week and then wrote the result it was always going to
 * write, so the claim is the result and a match with one is FINAL.
 */
export const LIVE_STATUSES = [MatchStatus.IN_PROGRESS];

/** What both Collection and KeyedCollection offer the store's bulk operations. */
interface Invalidatable {
  invalidateAll(): void;
  dispose(): void;
}

/** Composite keys. Kept in one place so invalidation and reads cannot drift. */
export const key = {
  standings: (familyId: string, gameId: string, range: StatsRange) =>
    `${familyId}|${gameId}|${range}`,
  headToHead: (familyId: string, userId: string, opponentId: string, gameId: string) =>
    `${familyId}|${userId}|${opponentId}|${gameId}`,
  profile: (familyId: string, userId: string) => `${familyId}|${userId}`,
};

/**
 * Store is the cache layer over the API: every piece of server-owned data the
 * app reads more than once lives here, shared by whichever presenters need it.
 *
 * Presenters read through these collections and write through `domain.api`.
 * After a write that changes the record, the presenter calls `invalidateAll()`
 * and every watching screen refreshes itself.
 */
export class Store {
  /** Families the signed-in player belongs to. */
  readonly families: Collection<Family[]>;

  readonly members: KeyedCollection<FamilyMember[]>;
  readonly games: KeyedCollection<Game[]>;

  /** Matches in play right now, keyed by family id. */
  readonly liveMatches: KeyedCollection<Match[]>;
  /** Finalized matches, newest first, keyed by family id. */
  readonly recentMatches: KeyedCollection<Match[]>;

  readonly standings: KeyedCollection<GetStandingsResponse | undefined>;
  readonly headToHead: KeyedCollection<GetHeadToHeadResponse | undefined>;
  readonly profiles: KeyedCollection<GetPlayerProfileResponse | undefined>;

  /** Everything the bulk refresh and teardown walk over. */
  private readonly _all: Invalidatable[];

  constructor(api: Clients) {
    this.families = new Collection<Family[]>([], async () => {
      const res = await api.families.listMyFamilies({});
      return res.families;
    });

    this.members = new KeyedCollection<FamilyMember[]>([], async (familyId) => {
      const res = await api.families.listMembers({ familyId });
      return res.members;
    });

    this.games = new KeyedCollection<Game[]>([], async (familyId) => {
      const res = await api.games.listGames({ familyId });
      return res.games;
    });

    this.liveMatches = new KeyedCollection<Match[]>([], async (familyId) => {
      const res = await api.matches.listMatches({ familyId, statuses: LIVE_STATUSES });
      return res.matches;
    });

    this.recentMatches = new KeyedCollection<Match[]>([], async (familyId) => {
      const res = await api.matches.listMatches({
        familyId,
        statuses: [MatchStatus.FINAL],
        limit: 25,
      });
      return res.matches;
    });

    this.standings = new KeyedCollection<GetStandingsResponse | undefined>(
      undefined,
      async (composite) => {
        const [familyId = "", gameId = "", range = "0"] = composite.split("|");
        return api.stats.getStandings({ familyId, gameId, range: Number(range) as StatsRange });
      },
    );

    this.headToHead = new KeyedCollection<GetHeadToHeadResponse | undefined>(
      undefined,
      async (composite) => {
        const [familyId = "", userId = "", opponentId = "", gameId = ""] = composite.split("|");
        return api.stats.getHeadToHead({ familyId, userId, opponentId, gameId });
      },
    );

    this.profiles = new KeyedCollection<GetPlayerProfileResponse | undefined>(
      undefined,
      async (composite) => {
        const [familyId = "", userId = ""] = composite.split("|");
        return api.stats.getPlayerProfile({ familyId, userId });
      },
    );

    this._all = [
      this.families,
      this.members,
      this.games,
      this.liveMatches,
      this.recentMatches,
      this.standings,
      this.headToHead,
      this.profiles,
    ];
  }

  /**
   * Mark everything stale. This is the "a match just finalized" signal —
   * standings, head-to-head records and profiles all change at once, and it is
   * not worth reasoning about which ones.
   */
  invalidateAll(): void {
    for (const c of this._all) c.invalidateAll();
  }

  /** Narrower refresh for things that change while a match is being played. */
  invalidateMatches(familyId: string): void {
    this.liveMatches.invalidate(familyId);
    this.recentMatches.invalidate(familyId);
  }

  dispose(): void {
    for (const c of this._all) c.dispose();
  }
}
