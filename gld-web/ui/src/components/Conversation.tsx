import { useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { ArrowDownToLine, ArrowUpToLine, Loader2, MoreHorizontal, Send, Square, SquareTerminal, Wrench } from "lucide-react"
import { useLocal } from "@/hooks/useLocal"
import { api, type Detail, type Msg, type Session } from "@/lib/api"
import { full, size, tilde } from "@/lib/format"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { useActions } from "./actions"
import { SessionDropdown } from "./SessionMenu"
import { Live } from "./SessionList"
import type { Row } from "./actions"
import { isTagged, Tagged } from "./Tagged"

// Messages de Claude : Markdown (tableaux GFM compris). react-markdown n'interprète jamais de HTML brut
// et filtre les URL dangereuses (javascript:) : rien de ce qui est affiché ne peut s'exécuter.
// onLink : intercepte un lien relatif (ex. une autre entrée de mémoire) ; renvoie false pour le laisser faire.
export function Markdown({ t, onLink }: { t: string; onLink?: (href: string) => boolean }) {
  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          // URL filtrée (javascript:…) : plus de href, donc du texte simple plutôt qu'un lien qui rouvrirait la page.
          a: ({ href, children }) =>
            href ? (
              <a href={href} target="_blank" rel="noopener noreferrer" onClick={(e) => onLink?.(href) && e.preventDefault()}>
                {children}
              </a>
            ) : (
              <span>{children}</span>
            ),
        }}
      >
        {t}
      </ReactMarkdown>
    </div>
  )
}

// Texte collé dans Claude Code (<pasted_content id="…">…</pasted_content id="…">) : un bloc repliable.
const PASTED = /<pasted_content id="[^"]*">\n?([\s\S]*?)\n?<\/pasted_content(?: id="[^"]*")?>/g

function UserText({ t }: { t: string }) {
  const parts: React.ReactNode[] = []
  let at = 0
  for (const m of t.matchAll(PASTED)) {
    const before = t.slice(at, m.index).trim()
    if (before) parts.push(<span key={`t${at}`}>{before}</span>)
    const lines = m[1].split("\n").length
    parts.push(
      <details key={m.index} className="group/paste my-1 rounded-lg border bg-background/60">
        <summary className="cursor-pointer list-none px-3 py-1.5 text-xs text-muted-foreground select-none hover:text-foreground">
          Texte collé · {lines} ligne{lines > 1 ? "s" : ""}
        </summary>
        <pre className="max-h-96 overflow-auto border-t px-3 py-2 text-xs whitespace-pre-wrap">{m[1]}</pre>
      </details>
    )
    at = m.index + m[0].length
  }
  const rest = t.slice(at).trim()
  if (rest) parts.push(<span key="rest">{rest}</span>)
  return <>{parts}</>
}

function Message({ m }: { m: Msg }) {
  const title = m.ts ? new Date(m.ts).toLocaleString("fr-FR") : undefined
  if (m.r === "user" && isTagged(m.t))
    return (
      <div title={title} className="msg ml-auto max-w-[85%]">
        <Tagged t={m.t} />
      </div>
    )
  if (m.r === "user")
    return (
      <div title={title} className="msg flex justify-end">
        <div className="max-w-[85%] rounded-2xl rounded-br-md bg-primary/[0.07] px-4 py-2.5 break-words whitespace-pre-wrap dark:bg-primary/10">
          <UserText t={m.t} />
        </div>
      </div>
    )
  if (m.r === "tool")
    return (
      <div title={title} className="msg border-l-2 py-0.5 pl-3 break-words whitespace-pre-wrap text-muted-foreground">
        {m.t}
      </div>
    )
  return (
    <div title={title} className="msg leading-relaxed break-words">
      <Markdown t={m.t} />
    </div>
  )
}

function Meta({ s, d }: { s: Session; d: Detail | null }) {
  const a = useActions()
  const live = a.live.has(s.id)
  const bits = [
    tilde(s.cwd, a.home),
    full(s.mtime),
    size(s.kb),
    d && `${d.n} prompt${d.n > 1 ? "s" : ""}`,
    d?.subagents ? `${d.subagents} sous-agent${d.subagents > 1 ? "s" : ""}` : null,
    d?.branch && `branche ${d.branch}`,
    d?.models.filter((m) => !m.startsWith("<")).join(", "),
  ].filter(Boolean)
  return (
    <p className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
      {live && <Live />}
      <span className="truncate">{(live ? "· " : "") + bits.join(" · ")}</span>
    </p>
  )
}

// Modes d'autorisation de claude -p : sans terminal, Claude ne peut pas demander la permission.
const MODES = [
  ["", "Lecture seule : écrire et lancer des commandes est refusé"],
  ["plan", "Plan : propose sans rien modifier"],
  ["acceptEdits", "Accepter les modifications de fichiers"],
  ["auto", "Auto : Claude décide de ce qu'il peut faire"],
] as const

type Turn = { id: string; msgs: Msg[]; running: boolean; error?: string; cost?: number }

// Écrire dans la session depuis le navigateur : claude -p --resume, réponse relayée au fil de l'eau (NDJSON).
function Composer({
  dir,
  id,
  args,
  live,
  turn,
  setTurn,
  onDone,
}: {
  dir: string
  id: string
  args: string
  live: boolean
  turn: Turn | null
  setTurn: (f: (t: Turn | null) => Turn | null) => void
  onDone: () => void
}) {
  const [text, setText] = useState("")
  const [mode, setMode] = useLocal("ccChatMode", "")
  const running = !!turn?.running
  const send = async () => {
    const prompt = text.trim()
    if (!prompt || running) return
    setText("")
    setTurn(() => ({ id, msgs: [{ r: "user", t: prompt }], running: true }))
    const fail = (error: string) => setTurn((t) => t && { ...t, running: false, error })
    let res: Response
    try {
      res = await fetch("/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ dir, id, prompt, args, mode }),
      })
    } catch (e) {
      return fail(e instanceof Error ? e.message : String(e))
    }
    if (!res.ok || !res.body) return fail(((await res.json().catch(() => ({}))) as { error?: string }).error ?? `HTTP ${res.status}`)
    const reader = res.body.getReader()
    const dec = new TextDecoder()
    let buf = ""
    for (;;) {
      const { value, done } = await reader.read()
      buf += dec.decode(value, { stream: !done })
      const lines = buf.split("\n")
      buf = lines.pop() ?? ""
      for (const l of lines) {
        if (!l.trim()) continue
        const e = JSON.parse(l) as { msgs?: Msg[]; done?: boolean; ok?: boolean; error?: string; cost?: number; note?: string }
        if (e.note) toast.info(e.note)
        setTurn(
          (t) =>
            t && {
              ...t,
              msgs: e.msgs ? [...t.msgs, ...e.msgs] : t.msgs,
              running: e.done ? false : t.running,
              error: e.done && !e.ok ? e.error || "Échec" : t.error,
              cost: e.cost ?? t.cost,
            }
        )
      }
      if (done) break
    }
    setTurn((t) => t && { ...t, running: false })
    onDone()
  }
  const stop = () => api.post("/chat-stop", { id })

  if (live)
    return (
      <p className="border-t px-6 py-3 text-center text-xs text-muted-foreground">
        Session ouverte dans un terminal : écris-y directement (deux processus ne peuvent pas écrire le même transcript).
      </p>
    )
  return (
    <div className="border-t bg-muted/20 px-4 py-3">
      <div className="mx-auto max-w-4xl space-y-2">
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "Enter" && (e.preventDefault(), send())}
          placeholder={running ? "Claude répond…" : "Écrire à Claude dans cette session… (Ctrl+Entrée pour envoyer)"}
          disabled={running}
          rows={2}
          maxLength={32000}
          className="session-text field-sizing-content max-h-48 min-h-14 w-full resize-none rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-60"
          aria-label="Message à Claude"
        />
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <select
            value={mode}
            onChange={(e) => setMode(e.target.value)}
            disabled={running}
            className="h-7 max-w-full rounded-md border bg-background px-2 text-xs"
            aria-label="Autorisations accordées à Claude"
          >
            {MODES.map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </select>
          {turn?.cost ? <span className="text-muted-foreground">coût indiqué par claude : {turn.cost.toFixed(2)} $</span> : null}
          <span className="ml-auto" />
          {running ? (
            <Button size="sm" variant="outline" onClick={stop}>
              <Square /> Arrêter
            </Button>
          ) : (
            <Button size="sm" onClick={send} disabled={!text.trim()}>
              <Send /> Envoyer
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

export function Conversation({ row, onClose, args }: { row: Row | null; onClose: () => void; args: string }) {
  const a = useActions()
  const session = row?.s ?? null
  // Les effets dépendent des identifiants, pas de l'objet : le rafraîchissement périodique de la liste
  // recrée les sessions sans que la conversation affichée doive être rechargée.
  const dir = session?.dir ?? ""
  const id = session?.id ?? ""
  const [tools, setTools] = useState(false)
  // Les messages sont rangés avec la clé (session, outils) qui les a produits : tant qu'elle ne correspond
  // pas à la demande courante, on affiche le chargement, sans remise à zéro synchrone dans un effet.
  const [loaded, setLoaded] = useState<{ key: string; msgs: Msg[] } | null>(null)
  const [info, setInfo] = useState<{ id: string; d: Detail } | null>(null)
  const area = useRef<HTMLDivElement>(null)
  const key = id ? `${dir}/${id}/${tools}` : ""
  const msgs = loaded?.key === key ? loaded.msgs : null
  const detail = info && info.id === id ? info.d : null
  // Tour en cours (ou dernier tour) écrit depuis le navigateur, affiché à la suite de la conversation.
  const [chat, setChat] = useState<Turn | null>(null)
  const turn = chat?.id === id ? chat : null
  const reload = () => api.conv({ dir, id }, tools).then((r) => setLoaded({ key, msgs: r.msgs }))

  useEffect(() => {
    if (!id) return
    let live = true
    api.conv({ dir, id }, tools).then((r) => live && setLoaded({ key, msgs: r.msgs }))
    return () => {
      live = false
    }
  }, [dir, id, tools, key])

  useEffect(() => {
    if (!id) return
    let live = true
    api.info({ dir, id }).then((d) => live && setInfo({ id, d }))
    return () => {
      live = false
    }
  }, [dir, id])

  const viewport = () => area.current?.querySelector<HTMLElement>('[data-slot="scroll-area-viewport"]')
  // content-visibility affine les hauteurs en cours de rendu : on recolle à la fin sur quelques images.
  const toEnd = () => {
    const vp = viewport()
    if (!vp) return
    const go = () => (vp.scrollTop = vp.scrollHeight)
    go()
    requestAnimationFrame(() => {
      go()
      requestAnimationFrame(() => {
        go()
        setTimeout(go, 120)
      })
    })
  }
  const toTop = () => viewport()?.scrollTo({ top: 0 })
  useEffect(() => {
    if (msgs) toEnd()
  }, [msgs])
  const shown = turn?.msgs.length ?? 0
  useEffect(() => {
    if (shown) toEnd()
  }, [shown])

  return (
    <Dialog open={!!session} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex h-[94vh] w-[96vw] max-w-none flex-col gap-0 overflow-hidden p-0 sm:max-w-none">
        <DialogHeader className="gap-1 border-b px-6 pt-5 pr-14 pb-4">
          <DialogTitle className="truncate text-base">{session?.title}</DialogTitle>
          <DialogDescription className="sr-only">Conversation complète de la session</DialogDescription>
          {session && <Meta s={session} d={detail} />}
        </DialogHeader>
        <div className="flex items-center gap-1 border-b bg-muted/30 px-4 py-2">
          <Button variant={tools ? "secondary" : "ghost"} size="sm" onClick={() => setTools(!tools)} aria-pressed={tools}>
            <Wrench /> Outils {tools ? "affichés" : "masqués"}
          </Button>
          <Button variant="ghost" size="icon-sm" onClick={toTop} aria-label="Aller au début" title="Aller au début">
            <ArrowUpToLine />
          </Button>
          <Button variant="ghost" size="icon-sm" onClick={toEnd} aria-label="Aller à la fin" title="Aller à la fin">
            <ArrowDownToLine />
          </Button>
          <div className="ml-auto flex items-center gap-1">
            {session && (
              <Button size="sm" onClick={() => a.open({ dir: session.dir, id: session.id })}>
                <SquareTerminal /> Reprendre dans un terminal
              </Button>
            )}
            {row && (
              <SessionDropdown row={row} after={onClose}>
                <Button variant="ghost" size="icon-sm" aria-label="Plus d'actions">
                  <MoreHorizontal />
                </Button>
              </SessionDropdown>
            )}
          </div>
        </div>
        <ScrollArea ref={area} className="min-h-0 flex-1">
          <div className="session-text mx-auto max-w-4xl space-y-5 px-6 py-6">
            {msgs === null && (
              <>
                <Skeleton className="ml-auto h-10 w-2/3" />
                <Skeleton className="h-24 w-full" />
                <Skeleton className="ml-auto h-10 w-1/2" />
              </>
            )}
            {msgs?.map((m, i) => (
              <Message key={i} m={m} />
            ))}
            {msgs?.length === 0 && !turn && <p className="text-center text-muted-foreground">Aucun message dans cette session.</p>}
            {turn?.msgs
              .filter((m) => tools || m.r !== "tool")
              .map((m, i) => (
                <Message key={`t${i}`} m={m} />
              ))}
            {turn?.running && (
              <p className="flex items-center gap-2 text-muted-foreground">
                <Loader2 className="size-4 animate-spin" /> Claude répond…
              </p>
            )}
            {turn?.error && <p className="rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-destructive">{turn.error}</p>}
          </div>
        </ScrollArea>
        {session && (
          <Composer
            dir={dir}
            id={id}
            args={args}
            live={a.live.has(id)}
            turn={turn}
            setTurn={(f) => setChat((c) => f(c?.id === id ? c : null))}
            onDone={() => {
              // Le transcript contient maintenant le tour : on le relit, et le tour affiché à part disparaît (sauf erreur).
              reload().then(() => setChat((c) => (c?.id === id && !c.error ? { ...c, msgs: [] } : c)))
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}
