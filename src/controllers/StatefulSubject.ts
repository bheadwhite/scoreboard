import { Subject } from "rxjs"

export default class StatefulSubject<T> extends Subject<T> {
  private state: T

  constructor(defaultState: T) {
    super()
    this.state = defaultState
  }

  public next(value: T) {
    this.state = value
    return super.next(value)
  }

  public getState() {
    return this.state
  }
}
