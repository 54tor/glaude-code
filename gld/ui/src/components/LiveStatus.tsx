import { useEffect, useState } from "react"
import { Bot, ChevronRight, Loader2 } from "lucide-react"
import { api, type LiveState, type Tokens } from "@/lib/api"
import { compact } from "@/lib/format"
import { cn } from "@/lib/utils"

const secs = (n: number) => (n < 60 ? `${Math.max(0, Math.round(n))} s` : n < 3600 ? `${Math.floor(n / 60)} min ${Math.round(n % 60)} s` : `${Math.floor(n / 3600)} h ${Math.floor((n % 3600) / 60)} min`)

// État de la session affichée : ce que Claude fait en ce moment (réflexion, outil, attente), consommation de tokens et sous-agents.
// Relu toutes les 2 s tant que la session est ouverte (fichiers locaux seulement), une seule fois sinon.
export function LiveStatus({ dir, id, open, tok }: { dir: string; id: string; open: boolean; tok: Tokens }) {
  const [st, setSt] = useState<LiveState | null>(null)
  const [now, setNow] = useState(() => Date.now() / 1000)
  const [subsOpen, setSubsOpen] = useState(true)
  useEffect(() => {
    let live = true
    const load = () => api.sessionLive(dir, id).then((r) => live && (setSt(r), setNow(Date.now() / 1000)), () => {})
    load()
    if (!open) return () => void (live = false)
    const t = setInterval(() => document.visibilityState === "visible" && load(), 2000)
    return () => {
      live = false
      clearInterval(t)
    }
  }, [dir, id, open])
  useEffect(() => {
    if (!open) return
    const t = setInterval(() => setNow(Date.now() / 1000), 1000) // le chrono avance entre deux relevés
    return () => clearInterval(t)
  }, [open])
  if (!st) return null
  const running = st.subs.filter((s) => s.running)
  if (!st.open && st.subs.length === 0) return null
  return (
    <div className="border-b bg-muted/20 px-6 py-2 text-xs">
      {st.open && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
          <span className={cn("flex items-center gap-1.5 font-medium", st.thinking ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400")}>
            {st.thinking ? <Loader2 className="size-3.5 animate-spin" /> : <span className="size-1.5 rounded-full bg-current" />}
            <span className="max-w-[60ch] truncate" title={st.activity}>
              {st.activity || (st.thinking ? "travaille" : "ouverte")}
            </span>
            {st.since > 0 && st.thinking && <span className="font-normal text-muted-foreground">depuis {secs(now - st.since)}</span>}
          </span>
          {st.context > 0 && (
            <span className="text-muted-foreground" title="Taille du contexte envoyé au dernier appel : entrée, cache lu et cache écrit">
              contexte ≈ {compact(st.context)} tokens
            </span>
          )}
          {st.out > 0 && <span className="text-muted-foreground">dernier appel : {compact(st.out)} produits</span>}
          {st.model && <span className="text-muted-foreground">{st.model}</span>}
        </div>
      )}
      <div className={cn("flex flex-wrap gap-x-4 gap-y-0.5 text-muted-foreground", st.open && "mt-1")} title="Total de la session">
        <span>entrée {compact(tok.in)}</span>
        <span>sortie {compact(tok.out)}</span>
        <span>cache lu {compact(tok.cr)}</span>
        <span>cache écrit {compact(tok.cw)}</span>
      </div>
      {st.subs.length > 0 && (
        <div className="mt-1">
          <button className="flex items-center gap-1 font-medium" onClick={() => setSubsOpen(!subsOpen)} aria-expanded={subsOpen}>
            <ChevronRight className={cn("size-3.5 transition-transform", subsOpen && "rotate-90")} />
            <Bot className="size-3.5" /> Sous-agents ({st.subs.length}
            {running.length > 0 && `, ${running.length} en cours`})
          </button>
          {subsOpen && (
            <ul className="mt-1 space-y-0.5 pl-5">
              {st.subs.map((s) => (
                <li key={s.id} className="flex flex-wrap items-center gap-x-2">
                  {s.running ? <Loader2 className="size-3 animate-spin text-emerald-600 dark:text-emerald-400" /> : <span className="size-1.5 rounded-full bg-muted-foreground/50" />}
                  <span className="font-medium">{s.desc || s.type}</span>
                  <span className="text-muted-foreground">{s.type}</span>
                  <span className="max-w-[50ch] truncate text-muted-foreground" title={s.activity}>
                    {s.activity}
                  </span>
                  {s.context > 0 && <span className="text-muted-foreground">· contexte {compact(s.context)}</span>}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}
