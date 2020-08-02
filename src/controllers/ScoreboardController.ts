import Player from "./Player"
import StatefulSubject from "./StatefulSubject"

export default class ScoreboardController {
  public players = new StatefulSubject([]) as StatefulSubject<Player[] | []>

  constructor(players?: number) {
    if (players == null) {
      return
    }
    Array.from({ length: players }).forEach(() => this.addNewPlayer())
  }

  addNewPlayer() {
    const id = this.players.getState().length + 1
    const player = new Player(id, `Player ${id}`)
    this.players.next([...this.players.getState(), player])
  }

  removePlayer(index: number) {
    const clone = this.players.getState().slice()
    clone.splice(index, 1)
    this.players.next(clone)
  }

  dispose() {
    this.players.complete()
  }
}
