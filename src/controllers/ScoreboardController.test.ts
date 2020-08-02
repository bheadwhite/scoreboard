import ScoreboardController from "./ScoreboardController"

describe("scoreboardController should...", () => {
  test("create x number of players on new instance", () => {
    const scoreboard = new ScoreboardController(2)

    expect(scoreboard.players).toHaveLength(2)
  })

  test("default name of players should be Player <num>", () => {
    const scoreboard = new ScoreboardController(2)

    expect(scoreboard.players.getState()[0].name).toBe("Player 1")
  })
})
