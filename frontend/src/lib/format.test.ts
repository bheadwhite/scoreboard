import { describe, expect, it } from "vitest";

import { ContractKind } from "../gen/scoreboard/v1/common_pb.js";
import { messageOf } from "../features/match/match_presenter.js";
import {
  contractText,
  initials,
  rivalryVerdict,
  streakLabel,
  winRateText,
} from "./format.js";

describe("initials", () => {
  it("takes the first and last initial", () => {
    expect(initials("Bill White")).toBe("BW");
    expect(initials("Sue Anne Baker")).toBe("SB");
  });

  it("handles one name", () => {
    expect(initials("Gran")).toBe("G");
  });

  it("never renders empty", () => {
    expect(initials("")).toBe("?");
    expect(initials("   ")).toBe("?");
  });
});

describe("contractText", () => {
  it("reads as plain English", () => {
    expect(contractText({ kind: ContractKind.FIRST_TO_SCORE, target: 500, detail: "" } as never)).toBe(
      "First to 500",
    );
    expect(contractText({ kind: ContractKind.BEST_OF, target: 3, detail: "" } as never)).toBe(
      "Best of 3",
    );
    expect(contractText({ kind: ContractKind.LAST_STANDING, target: 0, detail: "" } as never)).toBe(
      "Last one standing",
    );
  });

  it("uses the detail for custom rules", () => {
    expect(
      contractText({ kind: ContractKind.CUSTOM, target: 0, detail: "Chips at midnight" } as never),
    ).toBe("Chips at midnight");
  });

  it("says so when nothing is agreed", () => {
    expect(contractText(undefined)).toBe("No contract set");
  });
});

describe("streakLabel", () => {
  it("only calls out a streak worth bragging about", () => {
    expect(streakLabel(1)).toBe("");
    expect(streakLabel(0)).toBe("");
    expect(streakLabel(-1)).toBe("");
  });

  it("labels wins and losses", () => {
    expect(streakLabel(5)).toBe("W5");
    expect(streakLabel(-3)).toBe("L3");
  });
});

describe("winRateText", () => {
  it("has no opinion about a player who has not played", () => {
    expect(winRateText(0, 0)).toBe("—");
  });

  it("rounds to a whole percent", () => {
    expect(winRateText(1, 1)).toBe("50%");
    expect(winRateText(2, 1)).toBe("67%");
    expect(winRateText(3, 0)).toBe("100%");
  });
});

describe("rivalryVerdict", () => {
  it("is honest about never having played", () => {
    expect(rivalryVerdict(0, 0, "Dad")).toContain("never played");
  });

  it("calls out a tie", () => {
    expect(rivalryVerdict(3, 3, "Dad")).toContain("Dead even");
  });

  it("escalates with the margin", () => {
    expect(rivalryVerdict(3, 1, "Dad")).toBe("You are ahead of Dad.");
    expect(rivalryVerdict(9, 1, "Dad")).toBe("You own Dad.");
    expect(rivalryVerdict(1, 9, "Dad")).toBe("Dad owns you.");
  });

  it("handles a clean sweep in either direction", () => {
    expect(rivalryVerdict(4, 0, "Dad")).toBe("Undefeated against Dad.");
    expect(rivalryVerdict(0, 4, "Dad")).toBe("Dad has never lost to you.");
  });
});

describe("messageOf", () => {
  it("strips the Connect code prefix", () => {
    expect(messageOf(new Error("not found: that invite code does not match any family"))).toBe(
      "that invite code does not match any family",
    );
    expect(messageOf(new Error("failed_precondition: this match has already started"))).toBe(
      "this match has already started",
    );
    expect(messageOf(new Error("[permission_denied]: only the family organizer can do that"))).toBe(
      "only the family organizer can do that",
    );
  });

  it("leaves a message with an ordinary colon alone", () => {
    expect(messageOf(new Error("Points to win: 500"))).toBe("Points to win: 500");
  });

  it("falls back for anything that is not an Error", () => {
    expect(messageOf(undefined)).toBe("Something went wrong. Try again.");
    expect(messageOf("a bare string")).toBe("Something went wrong. Try again.");
  });
});
