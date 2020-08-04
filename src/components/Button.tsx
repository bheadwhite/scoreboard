import React from "react"
import useController from "src/hooks/useScoreboardController"
import { Button as MUIButton } from "@material-ui/core"

interface IProps {
  children?: React.ReactNode | React.ReactNode[]
}

const Button = ({ children }: IProps) => {
  const controller = useController()
  return (
    <MUIButton onClick={() => controller.addNewPlayer()}>{children}</MUIButton>
  )
}
export default Button
