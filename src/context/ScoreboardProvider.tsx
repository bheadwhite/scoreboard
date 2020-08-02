import React, { useMemo, createContext, useEffect } from "react"
import ScoreboardController from "src/controllers/ScoreboardController"

const scoreboardController = new ScoreboardController()
export const ScoreboardContext = createContext(scoreboardController)

interface Props {
  children: React.ReactNode[] | React.ReactNode
}
const ScoreboardProvider = ({ children }: Props) => {
  const scoreboardController = useMemo(() => new ScoreboardController(), [])
  useEffect(() => {
    return () => scoreboardController.dispose()
  }, [scoreboardController])

  return (
    <ScoreboardContext.Provider value={scoreboardController}>
      {children}
    </ScoreboardContext.Provider>
  )
}

export default ScoreboardProvider
