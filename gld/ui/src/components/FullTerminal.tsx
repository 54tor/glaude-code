import { useEffect, useRef, useState } from "react"
import { Terminal } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import "@xterm/xterm/css/xterm.css"
import { Eye, Pencil } from "lucide-react"
import { useTranslation } from "react-i18next"
import { i18n } from "@/i18n"
import { api } from "@/lib/api"
import { errMsg, v1 } from "@/lib/apiError"
import { Button } from "@/components/ui/button"

// Terminal complet de la session (tmux) : lecture seule tant qu'on ne « prend pas la main » ; le serveur ignore toute frappe avant.
// Le terminal épouse la zone d'affichage et la taille est répercutée sur la fenêtre tmux de l'hôte. La main est prise dès l'ouverture.
// takeHand=false (espace de travail) : ni main, ni focus, ni taille imposée à l'hôte tant que l'utilisateur ne clique pas sur le crayon.
export default function FullTerminal({ id, takeHand = true }: { id: string; takeHand?: boolean }) {
  const { t: tr } = useTranslation()
  const box = useRef<HTMLDivElement>(null)
  const ws = useRef<WebSocket | null>(null)
  const term = useRef<Terminal | null>(null)
  const [state, setState] = useState<"connexion" | "ouvert" | "fermé">("connexion")
  const [error, setError] = useState("")
  const [writable, setWritable] = useState(takeHand)
  const writableRef = useRef(takeHand)
  const sizeRef = useRef<() => void>(() => {})

  useEffect(() => {
    let dead = false
    let t: Terminal | null = null
    let ro: ResizeObserver | null = null
    api.termOpen(id).then((o) => {
      if (dead) return
      if (!o.token || !box.current) return setError(i18n.t("conversation.terminal.openFailed"))
      t = new Terminal({ fontSize: 13, cursorBlink: true, scrollback: 0, convertEol: false })
      const fit = new FitAddon()
      t.loadAddon(fit)
      term.current = t
      t.open(box.current)
      fit.fit()
      const sendSize = () => (takeHand || writableRef.current) && ws.current?.readyState === WebSocket.OPEN && t && ws.current.send(JSON.stringify({ resize: { cols: t.cols, rows: t.rows } }))
      sizeRef.current = sendSize
      ro = new ResizeObserver(() => (fit.fit(), sendSize()))
      ro.observe(box.current)
      const sock = new WebSocket(`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}${v1("/terminal/ws")}?t=${o.token}`)
      sock.binaryType = "arraybuffer"
      ws.current = sock
      sock.onopen = () => {
        setState("ouvert")
        sendSize()
        if (!takeHand) return
        sock.send(JSON.stringify({ write: true }))
        t?.focus()
      }
      sock.onmessage = (e) => t?.write(new Uint8Array(e.data as ArrayBuffer))
      sock.onclose = () => !dead && setState("fermé")
      const enc = new TextEncoder()
      t.onData((d) => writableRef.current && sock.readyState === WebSocket.OPEN && sock.send(enc.encode(d)))
    }, (e) => !dead && setError(errMsg(e) || i18n.t("conversation.terminal.openFailed")))
    return () => {
      dead = true
      ro?.disconnect()
      ws.current?.close()
      t?.dispose()
    }
  }, [id, takeHand])

  const setWrite = (on: boolean) => {
    if (ws.current?.readyState !== WebSocket.OPEN) return
    ws.current.send(JSON.stringify({ write: on }))
    writableRef.current = on
    setWritable(on)
    if (!on) return
    sizeRef.current()
    term.current?.focus()
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-3 border-b px-4 py-2">
        {state === "ouvert" && (
          <Button size="sm" variant={writable ? "secondary" : "outline"} onClick={() => setWrite(!writable)} title={writable ? tr("conversation.terminal.readOnlyTitle") : tr("conversation.terminal.writeTitle")}>
            {writable ? <Pencil /> : <Eye />} {writable ? tr("conversation.terminal.active") : tr("conversation.terminal.readOnly")}
          </Button>
        )}
        {state === "connexion" && !error && <span className="text-xs text-muted-foreground">{tr("conversation.terminal.connecting")}</span>}
        {state === "fermé" && <span className="text-xs text-muted-foreground">{tr("conversation.terminal.closed")}</span>}
      </div>
      {error && <p className="px-4 py-6 text-sm text-destructive">{error}</p>}
      <div className="min-h-0 flex-1 overflow-hidden bg-black p-2">
        <div ref={box} className="h-full w-full" />
      </div>
    </div>
  )
}
