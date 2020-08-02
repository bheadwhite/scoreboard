import React from "react"
import useController from "src/hooks/useScoreboardController"

const Button = () => {
  const controller = useController()
  return <div onClick={() => controller.addNewPlayer()}>add New Player</div>
}
export default Button
