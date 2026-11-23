import { useState } from "react"
import { NotificationDialog } from "./NotificationDialog"
import { StatusBar } from "./StatusBar"

export function NotificationHost() {
  const [target, setTarget] = useState<string | null | undefined>(undefined)
  return (
    <>
      <StatusBar onOpen={setTarget} />
      <NotificationDialog target={target} onTarget={setTarget} />
    </>
  )
}
