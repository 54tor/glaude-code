import { useEffect, useState } from "react"
import { api, type Detail } from "@/lib/api"
import { useActions, type Row } from "./actions"
import { LiveStats, useLive } from "./LiveStatus"

// Clic droit → Infos : la même fenêtre que le bouton « Stats » du lecteur (informations et statistiques réunies).
export function SessionInfo({ row, onClose }: { row: Row | null; onClose: () => void }) {
  const a = useActions()
  const [info, setInfo] = useState<{ id: string; d: Detail } | null>(null)
  const s = row?.s
  const dir = s?.dir ?? ""
  const id = s?.id ?? ""
  const live = useLive(dir, id, !!id && a.live.has(id))
  useEffect(() => {
    if (!dir || !id) return
    let alive = true
    api.info({ dir, id }).then((d) => alive && setInfo({ id, d }), () => {})
    return () => {
      alive = false
    }
  }, [dir, id])
  if (!row || !s) return null
  return <LiveStats live={live} s={s} project={row.g.project} detail={info?.id === id ? info.d : null} shown onShown={(o) => !o && onClose()} />
}
