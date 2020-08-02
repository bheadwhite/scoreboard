import React from "react"
import usePlayers from "src/hooks/usePlayers"
import useController from "src/hooks/useScoreboardController"

const Players = () => {
  const controller = useController()
  const players = usePlayers()

  const deletePlayer = (index: number) => {
    controller.removePlayer(index)
  }
  return (
    <div>
      {Array.from(players).map((player, i) => {
        return (
          <div style={{ display: "inline-block", width: "200", height: "200" }}>
            <div>name:{player.getName()}</div>
            <div>score:{player.getScore()}</div>
            <div onClick={() => player.setName("Gregory")}>rename to Greg</div>
            <div onClick={() => deletePlayer(i)}>delete player</div>
          </div>
        )
      })}
    </div>
  )
}

export default Players
