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
export function Tabs({ args, onAsk, onPrompt }: { args: string; onAsk: (ask: Ask) => void; onPrompt: (ask: PromptAsk) => void }) {
  const { t } = useTranslation()
  const actions = useActions()
  const [saves, setSaves] = useState<TabSave[] | null>(null)
  const refresh = () => api.tabs().then((tabsResponse) => setSaves(tabsResponse.saves))
  useEffect(() => {
    let isMounted = true
    api.tabs().then((tabsResponse) => isMounted && setSaves(tabsResponse.saves))
    return () => {
      isMounted = false
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
  const reopen = async (savedSessions: TabSession[]) => {
    const sessionsToReopen = savedSessions.filter((savedSession) => !actions.live.has(savedSession.id) && !isAuto(savedSession))
    if (!sessionsToReopen.length) return void toast.info(t("shell.tabs.allOpen"))
    const toastId = toast.loading(t("shell.tabs.opening", { sessions: t("shell.count.sessions", { count: sessionsToReopen.length }) }))
    let reopenedCount = 0
    for (const savedSession of sessionsToReopen) {
      const wasOpened = await api.open({ dir: savedSession.dir, id: savedSession.id, args }).then(
        () => true,
        () => false
      )
      if (wasOpened) reopenedCount++
    }
    const resultMessage = t("shell.tabs.reopened", { n: reopenedCount, total: sessionsToReopen.length })
    if (reopenedCount === sessionsToReopen.length) toast.success(resultMessage, { id: toastId })
    else toast.error(resultMessage, { id: toastId })
  }

  // Sauvegarde d'abord (la sauvegarde automatique saute les listes vides), puis ferme : « Tout rouvrir » reprend ce point.
  const closeAll = async () => {
    const liveSessionIds = [...actions.live]
    try {
      await api.tabsSave(t("shell.tabs.beforeClose"))
    } catch (error) {
      return void toast.error(errMsg(error))
    }
    await refresh()
    actions.stop(liveSessionIds, refresh)
  }

  const remove = (tabSave: TabSave) =>
    onAsk({
      title: t("shell.tabs.deleteTitle"),
      body: t("shell.tabs.deleteBody"),
      confirm: t("common.delete"),
      run: async () => {
        const ok = await reportAction(() => api.tabsRm(tabSave.at))
        refresh()
        return ok
      },
    })

  return (
    <ScrollArea className="min-h-0 flex-1">
      <div className="mx-auto max-w-page space-y-4 px-3 py-4 pb-24 md:px-6">
        <div className="flex flex-wrap items-center gap-3 rounded-lg border bg-muted/30 px-4 py-3 text-sm">
          <div className="min-w-0 flex-1">
            <p className="font-medium">
              {actions.live.size ? t("shell.tabs.openNow", { count: actions.live.size }) : t("shell.tabs.noneOpen")}
            </p>
            <p className="text-xs text-muted-foreground">
              {t("shell.tabs.autoNote")}{" "}
              <code className="rounded-sm bg-muted px-1">gld-matin --resume</code>
            </p>
          </div>
          <Button size="sm" onClick={save} disabled={!actions.live.size}>
            <Save /> {t("shell.tabs.saveNow")}
          </Button>
          <Button size="sm" variant="outline" onClick={closeAll} disabled={!actions.live.size}>
            <Power /> {t("shell.tabs.closeAll")}
          </Button>
          <Button size="sm" variant="outline" onClick={() => saves?.[0] && reopen(saves[0].sessions)} disabled={!saves?.length} title={t("shell.tabs.reopenLastTitle")}>
            <SquareTerminal /> {t("shell.tabs.reopenLast")}
          </Button>
        </div>

        {saves === null && <Skeleton className="h-24 w-full" />}
        {saves?.length === 0 && <p className="py-12 text-center text-sm text-muted-foreground">{t("shell.tabs.none")}</p>}
        {saves?.map((tabSave) => (
          <section key={tabSave.at} className="rounded-lg border">
            <div className="flex items-center gap-2 border-b px-4 py-2.5">
              <History className="size-4 shrink-0 text-muted-foreground" />
              <span className="truncate text-sm font-medium">{tabSave.name || t("shell.tabs.auto")}</span>
              {!tabSave.auto && <Badge variant="secondary">{t("shell.tabs.manual")}</Badge>}
              <span className="shrink-0 text-xs text-muted-foreground" title={full(tabSave.at)}>
                {ago(Date.parse(tabSave.at) / 1000)} · {t("shell.count.sessions", { count: tabSave.sessions.length })}
              </span>
              <Button variant="outline" size="xs" className="ml-auto" onClick={() => reopen(tabSave.sessions)}>
                <SquareTerminal /> {t("shell.tabs.reopenAll")}
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon-xs" aria-label={t("shell.tabs.actions")}>
                    <MoreHorizontal />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem variant="destructive" onSelect={() => remove(tabSave)}>
                    <Trash2 /> {t("shell.tabs.deleteSave")}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
            <ul className="divide-y text-sm">
              {tabSave.sessions.map((savedSession) => (
                <li key={savedSession.id} className="flex items-center gap-2 px-4 py-1.5">
                  <span className="min-w-0 flex-1 truncate">{savedSession.title}</span>
                  {actions.live.has(savedSession.id) && <Live className="text-xs" waiting={actions.waiting.has(savedSession.id)} />}
                  <span className="hidden shrink-0 truncate text-xs text-muted-foreground sm:block">{tilde(savedSession.project, actions.home)}</span>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </ScrollArea>
  )
}
