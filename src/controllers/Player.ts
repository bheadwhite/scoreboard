export interface PlayerInfoProps {
  name: string
  score: number
}

export default class Player {
  id: number
  name: string = ""
  score: number = 0

  constructor(id: number, name: string) {
    this.id = id
    this.name = name
  }

  setPlayerName(newName: string) {
    this.name = newName
  }
  setPlayerScore(score: number) {
    this.score = score
  }

  increaseScore() {
    this.score = this.score + 1
  }
  decreaseScore() {
    this.score = this.score - 1
  }
}
