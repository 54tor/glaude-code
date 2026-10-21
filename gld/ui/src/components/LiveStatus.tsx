import { useEffect, useState } from "react"
import { BarChart3, Bot, Loader2, Wrench } from "lucide-react"
import { api, type LiveState, type Tokens } from "@/lib/api"
import { compact, tokTotal } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

const secs = (n: number) => (n < 60 ? `${Math.max(0, Math.round(n))} s` : n < 3600 ? `${Math.floor(n / 60)} min ${Math.round(n % 60)} s` : `${Math.floor(n / 3600)} h ${Math.floor((n % 3600) / 60)} min`)

// État de la session affichée (relu toutes les 2 s tant qu'elle est ouverte) : useLive le lit, LiveBar montre ce que Claude fait,
// LiveStats (dans la barre d'outils) le contexte, les tokens et le bouton « Stats ». Ancien résumé : : ce que Claude fait en ce moment (réflexion, outil, attente) et un bouton « Stats » qui ouvre
// la consommation de tokens et les sous-agents dans une fenêtre. Relu toutes les 2 s tant que la session est ouverte (fichiers
// locaux seulement), une seule fois sinon.
export type Live = { st: LiveState | null; now: number; open: boolean }

export function useLive(dir: string, id: string, open: boolean): Live {
  const [st, setSt] = useState<LiveState | null>(null)
  const [now, setNow] = useState(() => Date.now() / 1000)
  useEffect(() => {
    if (!id) return
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
  return { st, now, open }
}

// Les états génériques (réflexion, travail) changent de verbe toutes les 6 s, l'un après l'autre, décalés d'une session à l'autre ;
// un outil en cours ou une attente se lisent tels quels.
const GENERIC = new Set(["", "réfléchit", "travaille", "réfléchit au résultat", "lit votre message et réfléchit"])
function label(st: LiveState, id: string, now: number) {
  if (!st.thinking || !GENERIC.has(st.activity) || !st.verbs?.length) return st.activity || (st.thinking ? "travaille" : "ouverte")
  const seed = [...id].reduce((n, c) => n + c.charCodeAt(0), 0)
  return st.verbs[(Math.floor(now / 6) + seed) % st.verbs.length] + "…"
}

// Ce que Claude fait en ce moment, en dernière ligne de la transcription. `open` (la liste des sessions ouvertes, mise à jour dès la fermeture) prime sur le relevé :
// le processus met un instant à finir après SIGTERM.
export function LiveBar({ live: { st, now, open }, id }: { live: Live; id: string }) {
  if (!st) return null
  if (!(open && st.open)) return null // fermée : « Session fermée » est dans la barre d'outils (LiveStats), sans bande
  const isOpen = true
  return (
    <div className="space-y-2">
      {st.queue?.map((m) => (
        <div key={m.ts + m.t} className="flex justify-end" title={new Date(m.ts).toLocaleString("fr-FR")}>
          <div className="max-w-[85%] rounded-2xl rounded-br-md border border-dashed bg-muted/30 px-4 py-2.5 break-words whitespace-pre-wrap text-muted-foreground">
            <p className="mb-0.5 text-[11px] font-medium tracking-wide uppercase">En attente de lecture par la session</p>
            {m.t}
          </div>
        </div>
      ))}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
        {isOpen && (
          <span className={cn("flex min-w-0 items-center gap-1.5 font-medium", st.thinking ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400")}>
            {st.thinking ? <Loader2 className="size-3.5 shrink-0 animate-spin" /> : <span className="size-1.5 shrink-0 rounded-full bg-current" />}
            <span className="truncate" title={st.activity}>
              {label(st, id, now)}
            </span>
            {st.since > 0 && st.thinking && <span className="shrink-0 font-normal text-muted-foreground">depuis {secs(now - st.since)}</span>}
          </span>
        )}
        {isOpen && st.tool && !st.activity.startsWith("exécute") && (
          <span className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground" title={st.tool}>
            <Wrench className="size-3 shrink-0" />
            <span className="shrink-0">{st.toolRunning ? "outil en cours" : "dernier outil"} :</span>
            <span className="truncate font-mono">{st.tool}</span>
          </span>
        )}
      </div>
    </div>
  )
}

// Contexte, tokens et sous-agents sur une ligne, le détail dans une fenêtre.
export function LiveStats({ live: { st, open }, tok }: { live: Live; tok: Tokens }) {
  const [stats, setStats] = useState(false)
  if (!st) return null
  return (
    <>
      <span className="ml-auto flex items-center gap-x-3 px-1 text-xs text-muted-foreground tabular-nums">
        {!(open && st.open) && <span className="font-medium">Session fermée</span>}
        {st.context > 0 && <span title="Contexte envoyé au dernier appel">ctx {compact(st.context)}</span>}
        <span title="Tokens traités (hors cache lu)">{compact(tokTotal(tok))} tok</span>
      </span>
      <Button size="sm" variant="ghost" onClick={() => setStats(true)} title="Tokens consommés et dernier appel">
        <BarChart3 /> Stats
      </Button>
      <Dialog open={stats} onOpenChange={setStats}>
        <DialogContent className="flex max-h-[85vh] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
          <DialogHeader className="border-b px-6 py-4">
            <DialogTitle>Statistiques de la session</DialogTitle>
            <DialogDescription className="sr-only">Tokens et contexte de la session</DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 space-y-5 overflow-x-hidden overflow-y-auto px-6 py-5 text-sm">
            <section>
              <h3 className="mb-1 font-medium">Consommation</h3>
              <dl className="grid grid-cols-2 gap-x-4 gap-y-1 sm:grid-cols-[auto_1fr_auto_1fr]">
                <dt className="text-muted-foreground">Entrée</dt>
                <dd className="tabular-nums">{compact(tok.in)}</dd>
                <dt className="text-muted-foreground">Sortie</dt>
                <dd className="tabular-nums">{compact(tok.out)}</dd>
                <dt className="text-muted-foreground">Cache lu</dt>
                <dd className="tabular-nums">{compact(tok.cr)}</dd>
                <dt className="text-muted-foreground">Cache écrit</dt>
                <dd className="tabular-nums">{compact(tok.cw)}</dd>
              </dl>
              <p className="mt-1 text-xs text-muted-foreground" title="Entrée neuve, sortie et cache écrit : le cache lu, relu à chaque tour, est compté à part">
                Traité : {compact(tokTotal(tok))} tokens (hors cache lu)
              </p>
            </section>
            {(st.context > 0 || st.model) && (
              <section>
                <h3 className="mb-1 font-medium">Dernier appel</h3>
                {st.model && (
                  <p>
                    Modèle : <code className="text-xs">{st.model}</code>
                  </p>
                )}
                {st.context > 0 && <p>Contexte envoyé : ≈ {compact(st.context)} tokens (entrée, cache lu et cache écrit)</p>}
                {st.out > 0 && <p>Tokens produits : {compact(st.out)}</p>}
              </section>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

// Sous-agents de la session : un bouton (nombre et agents en cours) qui ouvre la liste, à côté des commandes de la session.
export function LiveAgents({ live: { st } }: { live: Live }) {
  const [shown, setShown] = useState(false)
  if (!st || st.subs.length === 0) return null
  const running = st.subs.filter((s) => s.running).length
  return (
    <>
      <Button size="sm" variant="ghost" onClick={() => setShown(true)} title="Sous-agents de la session">
        <Bot /> Sous-agents · {st.subs.length}
        {running > 0 && <span className="text-emerald-600 dark:text-emerald-400">({running} en cours)</span>}
      </Button>
      <Dialog open={shown} onOpenChange={setShown}>
        <DialogContent className="flex max-h-[85vh] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
          <DialogHeader className="border-b px-6 py-4">
            <DialogTitle>
              Sous-agents ({st.subs.length}
              {running > 0 && `, ${running} en cours`})
            </DialogTitle>
            <DialogDescription className="sr-only">Sous-agents lancés par la session</DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto px-6 py-5 text-sm">
            <ul className="space-y-1.5">
              {st.subs.map((s) => (
                <li key={s.id} className="flex flex-wrap items-center gap-x-2">
                  {s.running ? <Loader2 className="size-3.5 animate-spin text-emerald-600 dark:text-emerald-400" /> : <span className="size-1.5 rounded-full bg-muted-foreground/50" />}
                  <span className="font-medium">{s.desc || s.type}</span>
                  <span className="text-xs text-muted-foreground">{s.type}</span>
                  <span className="min-w-0 truncate text-xs text-muted-foreground" title={s.activity}>
                    {s.activity}
                  </span>
                  {s.context > 0 && <span className="text-xs text-muted-foreground">· contexte {compact(s.context)}</span>}
                </li>
              ))}
            </ul>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
