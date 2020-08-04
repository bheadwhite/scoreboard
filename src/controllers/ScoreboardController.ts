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

  getPlayers() {
    return this.players.getState()
  }

  addNewPlayer() {
    const id = this.players.getState().length
    const player = new Player(id, `Player ${id + 1}`)
    this.players.next([...this.players.getState(), player])
  }

  removePlayer(index) {
    const clone = this.players.getState().slice()
    clone.splice(index, 1)
    this.players.next(clone)
  }

  setPlayerName(i: number, newName: string) {
    const players = this.getPlayers().slice()
    players[i].setPlayerName(newName)
    this.players.next(players)
  }

  increasePlayerScore(i: number) {
    const players = this.getPlayers().slice()
    players[i].increaseScore()
    this.players.next(players)
  }
  decreasePlayerScore(i: number) {
    const players = this.getPlayers().slice()
    players[i].decreaseScore()
    this.players.next(players)
  }

  onPlayersChange(callback: (players: Player[]) => void) {
    return this.players.subscribe({
      next: callback,
    })
  }

  dispose() {
    this.players.complete()
  }
}
