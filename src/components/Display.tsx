import React, { useState } from "react"
import usePlayers from "src/hooks/usePlayers"
import useController from "src/hooks/useScoreboardController"
import { Button, TextField } from "@material-ui/core"

const Players = () => {
  const controller = useController()
  const players = usePlayers()
  const [name, setName] = useState<string>("")
  const [edit, setEdit] = useState<boolean>(false)
  const handleEditName = (name) => {
    name && setName(name)
    setEdit((a) => !a)
  }
  const handleChangeName = (e) => {
    setName(e.target.value)
  }
  const saveName = (id) => {
    controller.setPlayerName(id, name)
    setEdit((a) => !a)
  }

  return (
    <div style={{ display: "flex" }}>
      {Array.from(players).map((player, i) => {
        return (
          <div key={player.name} style={{ display: "inlineBlock", width: 200 }}>
            {edit ? (
              <>
                <TextField value={name} onChange={handleChangeName} />
                <Button onClick={() => saveName(i)}>Save</Button>
              </>
            ) : (
              <div onClick={() => handleEditName(player.name)}>
                name: {player.name}
              </div>
            )}
            <div>score: {player.score}</div>
            <Button onClick={() => controller.removePlayer(i)}>delete</Button>
            <Button onClick={() => controller.setPlayerName(i, "greg")}>
              rename to greg
            </Button>
          </div>
        )
      })}
    </div>
  )
}

export default Players
