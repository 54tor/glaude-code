import { useEffect, useRef, useState } from "react"
import { Terminal } from "@xterm/xterm"
import "@xterm/xterm/css/xterm.css"
import { Eye, Pencil } from "lucide-react"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"

// Terminal complet de la session (tmux) : lecture seule tant qu'on ne « prend pas la main » ; le serveur ignore toute frappe avant.
// La taille est celle de la fenêtre tmux de l'hôte (le terminal de l'hôte n'est pas redimensionné) ; la zone défile si elle est plus grande.
export default function FullTerminal({ id }: { id: string }) {
  const box = useRef<HTMLDivElement>(null)
  const ws = useRef<WebSocket | null>(null)
  const term = useRef<Terminal | null>(null)
  const [state, setState] = useState<"connexion" | "ouvert" | "fermé">("connexion")
  const [error, setError] = useState("")
  const [writable, setWritable] = useState(false)
  const writableRef = useRef(false)

  useEffect(() => {
    let dead = false
    let t: Terminal | null = null
    api.post("/term-open", { id }).then((r) => {
      const o = r as unknown as { ok: boolean; out: string; token?: string; cols?: number; rows?: number }
      if (dead) return
      if (!o.ok || !o.token || !box.current) return setError(o.out || "ouverture impossible")
      t = new Terminal({ cols: o.cols, rows: o.rows, fontSize: 13, cursorBlink: true, scrollback: 0, convertEol: false })
      term.current = t
      t.open(box.current)
      const sock = new WebSocket(`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/term-ws?t=${o.token}`)
      sock.binaryType = "arraybuffer"
      ws.current = sock
      sock.onopen = () => setState("ouvert")
      sock.onmessage = (e) => t?.write(new Uint8Array(e.data as ArrayBuffer))
      sock.onclose = () => !dead && setState("fermé")
      const enc = new TextEncoder()
      t.onData((d) => writableRef.current && sock.readyState === WebSocket.OPEN && sock.send(enc.encode(d)))
    })
    return () => {
      dead = true
      ws.current?.close()
      t?.dispose()
    }
  }, [id])

  const setWrite = (on: boolean) => {
    if (ws.current?.readyState !== WebSocket.OPEN) return
    ws.current.send(JSON.stringify({ write: on }))
    writableRef.current = on
    setWritable(on)
    if (on) term.current?.focus()
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-3 border-b px-4 py-2">
        {state === "ouvert" && (
          <Button size="sm" variant={writable ? "secondary" : "outline"} onClick={() => setWrite(!writable)} title={writable ? "Repasser en lecture seule" : "Autoriser la saisie dans le terminal"}>
            {writable ? <Pencil /> : <Eye />} {writable ? "Saisie active · repasser en lecture seule" : "Lecture seule · prendre la main"}
          </Button>
        )}
        {state === "connexion" && !error && <span className="text-xs text-muted-foreground">Connexion…</span>}
        {state === "fermé" && <span className="text-xs text-muted-foreground">Connexion fermée</span>}
      </div>
      {error && <p className="px-4 py-6 text-sm text-destructive">{error}</p>}
      <div className="min-h-0 flex-1 overflow-auto bg-black p-2">
        <div ref={box} />
      </div>
    </div>
  )
}
