import { useEffect, useState } from "react"
import { History, MoreHorizontal, Save, SquareTerminal, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { api, type TabSave, type TabSession } from "@/lib/api"
import { ago, full, tilde } from "@/lib/format"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { useActions } from "./actions"
import type { Ask } from "./Confirm"
import type { Ask as PromptAsk } from "./Prompt"
import { Live } from "./SessionList"

const plural = (n: number) => `${n} session${n > 1 ? "s" : ""}`

// Sessions ouvertes sauvegardées (automatiquement toutes les 2 min, ou à la main avec un nom), à rouvrir d'un coup.
export function Tabs({ args, onAsk, onPrompt }: { args: string; onAsk: (a: Ask) => void; onPrompt: (a: PromptAsk) => void }) {
  const a = useActions()
  const [saves, setSaves] = useState<TabSave[] | null>(null)
  const refresh = () => api.tabs().then((r) => setSaves(r.saves))
  useEffect(() => {
    let live = true
    api.tabs().then((r) => live && setSaves(r.saves))
    return () => {
      live = false
    }
  }, [])

  const save = () =>
    onPrompt({
      title: "Sauvegarder les sessions ouvertes",
      value: "",
      placeholder: "Nom (facultatif), ex. avant redémarrage",
      optional: true,
      confirm: "Sauvegarder",
      run: async (name) => {
        const r = await api.post("/tabs-save", { name })
        ;(r.ok ? toast.success : toast.error)(r.out)
        refresh()
      },
    })

  // Rouvre les sessions de la sauvegarde qui ne sont pas déjà ouvertes, l'une après l'autre.
  const reopen = async (ss: TabSession[]) => {
    const todo = ss.filter((s) => !a.live.has(s.id))
    if (!todo.length) return void toast.info("Toutes ces sessions sont déjà ouvertes")
    const id = toast.loading(`Ouverture de ${plural(todo.length)}…`)
    let n = 0
    for (const s of todo) if ((await api.post("/open", { dir: s.dir, id: s.id, args })).ok) n++
    ;(n === todo.length ? toast.success : toast.error)(`${n} / ${todo.length} rouverte(s)`, { id })
  }

  const remove = (s: TabSave) =>
    onAsk({
      title: "Supprimer cette sauvegarde ?",
      body: "Seule la sauvegarde disparaît : les sessions restent intactes.",
      confirm: "Supprimer",
      run: async () => {
        const r = await api.post("/tabs-rm", { at: s.at })
        ;(r.ok ? toast.success : toast.error)(r.out)
        refresh()
      },
    })

  return (
    <ScrollArea className="min-h-0 flex-1">
      <div className="mx-auto max-w-7xl space-y-4 px-3 py-4 pb-24 md:px-6">
        <div className="flex flex-wrap items-center gap-3 rounded-xl border bg-muted/30 px-4 py-3 text-sm">
          <div className="min-w-0 flex-1">
            <p className="font-medium">
              {a.live.size ? `${plural(a.live.size)} ouverte${a.live.size > 1 ? "s" : ""} en ce moment` : "Aucune session ouverte"}
            </p>
            <p className="text-xs text-muted-foreground">
              Sauvegarde automatique toutes les 2 minutes tant que gld-web tourne, gardée une heure (la plus récente toujours). Après un redémarrage :{" "}
              <code className="rounded bg-muted px-1">gld-matin --resume</code>
            </p>
          </div>
          <Button size="sm" onClick={save} disabled={!a.live.size}>
            <Save /> Sauvegarder maintenant…
          </Button>
        </div>

        {saves === null && <Skeleton className="h-24 w-full" />}
        {saves?.length === 0 && <p className="py-12 text-center text-sm text-muted-foreground">Aucune sauvegarde pour l'instant.</p>}
        {saves?.map((s) => (
          <section key={s.at} className="rounded-xl border">
            <div className="flex items-center gap-2 border-b px-4 py-2.5">
              <History className="size-4 shrink-0 text-muted-foreground" />
              <span className="truncate text-sm font-medium">{s.name || "Sauvegarde automatique"}</span>
              {!s.auto && <Badge variant="secondary">manuelle</Badge>}
              <span className="shrink-0 text-xs text-muted-foreground" title={full(s.at)}>
                {ago(Date.parse(s.at) / 1000)} · {plural(s.sessions.length)}
              </span>
              <Button variant="outline" size="xs" className="ml-auto" onClick={() => reopen(s.sessions)}>
                <SquareTerminal /> Tout rouvrir
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon-xs" aria-label="Actions de la sauvegarde">
                    <MoreHorizontal />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem variant="destructive" onSelect={() => remove(s)}>
                    <Trash2 /> Supprimer la sauvegarde
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
            <ul className="divide-y text-sm">
              {s.sessions.map((t) => (
                <li key={t.id} className="flex items-center gap-2 px-4 py-1.5">
                  <span className="min-w-0 flex-1 truncate">{t.title}</span>
                  {a.live.has(t.id) && <Live className="text-xs" waiting={a.waiting.has(t.id)} />}
                  <span className="hidden shrink-0 truncate text-xs text-muted-foreground sm:block">{tilde(t.project, a.home)}</span>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </ScrollArea>
  )
}
