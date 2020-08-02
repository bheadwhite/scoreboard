import { useContext } from "react"
import { ScoreboardContext } from "src/context/ScoreboardProvider"

const useScoreboardController = () => {
  return useContext(ScoreboardContext)
}

export default useScoreboardController
