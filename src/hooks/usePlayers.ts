import { useState, useEffect, useMemo } from "react"
import useScoreboardController from "src/hooks/useScoreboardController"
import Player from "src/controllers/Player"

const usePlayers = () => {
  const controller = useScoreboardController()
  const [players, setPlayers] = useState<Player[] | []>(controller.getPlayers())

  const subscription = useMemo(() => {
    return controller.onPlayersChange((changes) => {
      setPlayers(changes)
    })
  }, [controller])

  useEffect(() => {
    return () => subscription.unsubscribe()
  }, [subscription])

  return players
}

export default usePlayers
