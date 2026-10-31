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
  const { t } = useTranslation()
  const containerRef = useRef<HTMLDivElement>(null)
  const socketRef = useRef<WebSocket | null>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const [state, setState] = useState<"connexion" | "ouvert" | "fermé">("connexion")
  const [error, setError] = useState("")
  const [writable, setWritable] = useState(takeHand)
  const writableRef = useRef(takeHand)
  const sendSizeRef = useRef<() => void>(() => {})

  useEffect(() => {
    let isDisposed = false
    let terminal: Terminal | null = null
    let resizeObserver: ResizeObserver | null = null
    api.termOpen(id).then(
      (openResponse) => {
        if (isDisposed) return
        if (!openResponse.token || !containerRef.current) return setError(i18n.t("conversation.terminal.openFailed"))
        terminal = new Terminal({ fontSize: 13, cursorBlink: true, scrollback: 0, convertEol: false })
        const fitAddon = new FitAddon()
        terminal.loadAddon(fitAddon)
        terminalRef.current = terminal
        terminal.open(containerRef.current)
        fitAddon.fit()
        const sendSize = () => {
          const mayImposeSize = takeHand || writableRef.current
          if (!mayImposeSize) return
          if (socketRef.current?.readyState !== WebSocket.OPEN || !terminal) return
          socketRef.current.send(JSON.stringify({ resize: { cols: terminal.cols, rows: terminal.rows } }))
        }
        sendSizeRef.current = sendSize
        resizeObserver = new ResizeObserver(() => {
          fitAddon.fit()
          sendSize()
        })
        resizeObserver.observe(containerRef.current)
        const socketScheme = location.protocol === "https:" ? "wss" : "ws"
        const socket = new WebSocket(`${socketScheme}://${location.host}${v1("/terminal/ws")}?t=${openResponse.token}`)
        socket.binaryType = "arraybuffer"
        socketRef.current = socket
        socket.onopen = () => {
          setState("ouvert")
          sendSize()
          if (!takeHand) return
          socket.send(JSON.stringify({ write: true }))
          terminal?.focus()
        }
        socket.onmessage = (messageEvent) => terminal?.write(new Uint8Array(messageEvent.data as ArrayBuffer))
        socket.onclose = () => !isDisposed && setState("fermé")
        const textEncoder = new TextEncoder()
        terminal.onData((typedData) => {
          if (!writableRef.current || socket.readyState !== WebSocket.OPEN) return
          socket.send(textEncoder.encode(typedData))
        })
      },
      (openError) => !isDisposed && setError(errMsg(openError) || i18n.t("conversation.terminal.openFailed"))
    )
    return () => {
      isDisposed = true
      resizeObserver?.disconnect()
      socketRef.current?.close()
      terminal?.dispose()
    }
  }, [id, takeHand])

  const setWrite = (isWriting: boolean) => {
    if (socketRef.current?.readyState !== WebSocket.OPEN) return
    socketRef.current.send(JSON.stringify({ write: isWriting }))
    writableRef.current = isWriting
    setWritable(isWriting)
    if (!isWriting) return
    sendSizeRef.current()
    terminalRef.current?.focus()
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-3 border-b px-4 py-2">
        {state === "ouvert" && (
          <Button size="sm" variant={writable ? "secondary" : "outline"} onClick={() => setWrite(!writable)} title={writable ? t("conversation.terminal.readOnlyTitle") : t("conversation.terminal.writeTitle")}>
            {writable ? <Pencil /> : <Eye />} {writable ? t("conversation.terminal.active") : t("conversation.terminal.readOnly")}
          </Button>
        )}
        {state === "connexion" && !error && <span className="text-xs text-muted-foreground">{t("conversation.terminal.connecting")}</span>}
        {state === "fermé" && <span className="text-xs text-muted-foreground">{t("conversation.terminal.closed")}</span>}
      </div>
      {error && <p className="px-4 py-6 text-sm text-destructive">{error}</p>}
      <div className="min-h-0 flex-1 overflow-hidden bg-black p-2">
        <div ref={containerRef} className="h-full w-full" />
      </div>
    </div>
  )
}
