import { useEffect, useState } from "react"
import { api, type Detail } from "@/lib/api"
import { useActions, type Row } from "./actions"
import { LiveStats, useLive } from "./LiveStatus"

// Clic droit → Infos : la même fenêtre que le bouton « Stats » du lecteur (informations et statistiques réunies).
export function SessionInfo({ row, onClose }: { row: Row | null; onClose: () => void }) {
  const actions = useActions()
  const [info, setInfo] = useState<{ id: string; detail: Detail } | null>(null)
  const session = row?.s
  const dir = session?.dir ?? ""
  const id = session?.id ?? ""
  const live = useLive(dir, id, !!id && actions.live.has(id))
  useEffect(() => {
    if (!dir || !id) return
    let isMounted = true
    api.info({ dir, id }).then((detail) => isMounted && setInfo({ id, detail }), () => {})
    return () => {
      isMounted = false
    }
  }, [dir, id])
  if (!row || !session) return null
  const detail = info?.id === id ? info.detail : null
  return <LiveStats live={live} s={session} project={row.g.project} detail={detail} shown onShown={(isShown) => !isShown && onClose()} />
}
