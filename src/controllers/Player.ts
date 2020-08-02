export default class Player {
  id: number
  name: string
  score: number = 0

  constructor(id: number, name: string) {
    this.id = id
    this.name = name
  }

  setName(newName: string) {
    this.name = newName
  }
  increaseScore() {
    this.score = this.score + 1
  }
  decreaseScore() {
    this.score = this.score - 1
  }
}
