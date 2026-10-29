import { useEffect, useState } from "react"
import { History, MoreHorizontal, Power, Save, SquareTerminal, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
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
import { reportAction } from "@/lib/report"
import { errMsg } from "@/lib/apiError"
import { isAuto } from "@/lib/sessionsView"

// Sessions ouvertes sauvegardées (automatiquement toutes les 2 min, ou à la main avec un nom), à rouvrir d'un coup.
export function Tabs({ args, onAsk, onPrompt }: { args: string; onAsk: (a: Ask) => void; onPrompt: (a: PromptAsk) => void }) {
  const { t } = useTranslation()
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
      title: t("shell.tabs.saveTitle"),
      value: "",
      placeholder: t("shell.tabs.savePlaceholder"),
      optional: true,
      confirm: t("common.save"),
      run: async (name) => {
        const ok = await reportAction(() => api.tabsSave(name))
        refresh()
        return ok
      },
    })

  // Rouvre les sessions de la sauvegarde qui ne sont pas déjà ouvertes, l'une après l'autre.
  const reopen = async (ss: TabSession[]) => {
    const todo = ss.filter((s) => !a.live.has(s.id) && !isAuto(s))
    if (!todo.length) return void toast.info(t("shell.tabs.allOpen"))
    const id = toast.loading(t("shell.tabs.opening", { sessions: t("shell.count.sessions", { count: todo.length }) }))
    let n = 0
    for (const s of todo) if (await api.open({ dir: s.dir, id: s.id, args }).then(() => true, () => false)) n++
    ;(n === todo.length ? toast.success : toast.error)(t("shell.tabs.reopened", { n, total: todo.length }), { id })
  }

  // Sauvegarde d'abord (la sauvegarde automatique saute les listes vides), puis ferme : « Tout rouvrir » reprend ce point.
  const closeAll = async () => {
    const ids = [...a.live]
    try {
      await api.tabsSave(t("shell.tabs.beforeClose"))
    } catch (e) {
      return void toast.error(errMsg(e))
    }
    await refresh()
    a.stop(ids, refresh)
  }

  const remove = (s: TabSave) =>
    onAsk({
      title: t("shell.tabs.deleteTitle"),
      body: t("shell.tabs.deleteBody"),
      confirm: t("common.delete"),
      run: async () => {
        const ok = await reportAction(() => api.tabsRm(s.at))
        refresh()
        return ok
      },
    })

  return (
    <ScrollArea className="min-h-0 flex-1">
      <div className="mx-auto max-w-7xl space-y-4 px-3 py-4 pb-24 md:px-6">
        <div className="flex flex-wrap items-center gap-3 rounded-lg border bg-muted/30 px-4 py-3 text-sm">
          <div className="min-w-0 flex-1">
            <p className="font-medium">
              {a.live.size ? t("shell.tabs.openNow", { count: a.live.size }) : t("shell.tabs.noneOpen")}
            </p>
            <p className="text-xs text-muted-foreground">
              {t("shell.tabs.autoNote")}{" "}
              <code className="rounded-sm bg-muted px-1">gld-matin --resume</code>
            </p>
          </div>
          <Button size="sm" onClick={save} disabled={!a.live.size}>
            <Save /> {t("shell.tabs.saveNow")}
          </Button>
          <Button size="sm" variant="outline" onClick={closeAll} disabled={!a.live.size}>
            <Power /> {t("shell.tabs.closeAll")}
          </Button>
          <Button size="sm" variant="outline" onClick={() => saves?.[0] && reopen(saves[0].sessions)} disabled={!saves?.length} title={t("shell.tabs.reopenLastTitle")}>
            <SquareTerminal /> {t("shell.tabs.reopenLast")}
          </Button>
        </div>

        {saves === null && <Skeleton className="h-24 w-full" />}
        {saves?.length === 0 && <p className="py-12 text-center text-sm text-muted-foreground">{t("shell.tabs.none")}</p>}
        {saves?.map((s) => (
          <section key={s.at} className="rounded-lg border">
            <div className="flex items-center gap-2 border-b px-4 py-2.5">
              <History className="size-4 shrink-0 text-muted-foreground" />
              <span className="truncate text-sm font-medium">{s.name || t("shell.tabs.auto")}</span>
              {!s.auto && <Badge variant="secondary">{t("shell.tabs.manual")}</Badge>}
              <span className="shrink-0 text-xs text-muted-foreground" title={full(s.at)}>
                {ago(Date.parse(s.at) / 1000)} · {t("shell.count.sessions", { count: s.sessions.length })}
              </span>
              <Button variant="outline" size="xs" className="ml-auto" onClick={() => reopen(s.sessions)}>
                <SquareTerminal /> {t("shell.tabs.reopenAll")}
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon-xs" aria-label={t("shell.tabs.actions")}>
                    <MoreHorizontal />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem variant="destructive" onSelect={() => remove(s)}>
                    <Trash2 /> {t("shell.tabs.deleteSave")}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
            <ul className="divide-y text-sm">
              {s.sessions.map((ts) => (
                <li key={ts.id} className="flex items-center gap-2 px-4 py-1.5">
                  <span className="min-w-0 flex-1 truncate">{ts.title}</span>
                  {a.live.has(ts.id) && <Live className="text-xs" waiting={a.waiting.has(ts.id)} />}
                  <span className="hidden shrink-0 truncate text-xs text-muted-foreground sm:block">{tilde(ts.project, a.home)}</span>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </ScrollArea>
  )
}
