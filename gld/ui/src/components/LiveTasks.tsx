import { useTranslation } from "react-i18next"
import { useEffect, useState } from "react"
import { CircleAlert, CircleCheck, CircleHelp, ListChecks, Loader2 } from "lucide-react"
import { api, type BgTask } from "@/lib/api"
import { ago } from "@/lib/format"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import type { Live } from "./LiveStatus"

function Icon({ s }: { s: BgTask["status"] }) {
  if (s === "running") return <Loader2 className="size-3.5 shrink-0 animate-spin text-success" />
  if (s === "completed") return <CircleCheck className="size-3.5 shrink-0 text-success" />
  if (s === "unknown") return <CircleHelp className="size-3.5 shrink-0 text-muted-foreground" />
  return <CircleAlert className="size-3.5 shrink-0 text-warning" />
}

// Tâches de fond de la session (commandes Bash en arrière-plan, Monitor, workflows) relues dans son transcript : un bouton avec le
// nombre de tâches en cours, la liste au clic, et la sortie de chacune, relue toutes les 3 s tant qu'elle tourne.
export function LiveTasks({ live: { st }, dir, id }: { live: Live; dir: string; id: string }) {
  const { t } = useTranslation()
  const [shown, setShown] = useState(false)
  const [pick, setPick] = useState<BgTask | null>(null)
  const tasks = st?.tasks ?? []
  const running = tasks.filter((x) => x.status === "running").length
  const cur = pick && tasks.find((x) => x.id === pick.id)
  const [out, setOut] = useState<{ id: string; text: string; ok: boolean } | null>(null)
  const watch = cur?.status === "running"
  useEffect(() => {
    if (!pick) return
    let live = true
    const load = () => api.taskOutput(dir, id, pick.id).then((r) => live && setOut({ id: pick.id, text: r.text ?? "", ok: r.ok }), () => {})
    load()
    if (!watch) return () => void (live = false)
    const iv = setInterval(() => document.visibilityState === "visible" && load(), 3000)
    return () => {
      live = false
      clearInterval(iv)
    }
  }, [pick, watch, dir, id])
  if (tasks.length === 0) return null
  return (
    <>
      <Button size="xs" variant="ghost" onClick={() => setShown(true)} title={t("conversation.tasks.title")}>
        <ListChecks /> {t("conversation.tasks.count", { n: tasks.length })}
        {running > 0 && <span className="text-success">{t("conversation.live.runningCount", { running })}</span>}
      </Button>
      <Dialog open={shown} onOpenChange={setShown}>
        <DialogContent className="flex h-[85vh] max-h-[85vh] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
          <DialogHeader className="border-b px-6 py-4">
            <DialogTitle>{t("conversation.tasks.dialogTitle", { n: tasks.length })}</DialogTitle>
            <DialogDescription>{t("conversation.tasks.description")}</DialogDescription>
          </DialogHeader>
          <ScrollArea className="min-h-0 flex-1">
          <ul className="space-y-1 px-4 py-4 text-sm">
            {[...tasks].reverse().map((x) => (
              <li key={x.id}>
                <button type="button" onClick={() => setPick(x)} className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-muted" title={t("conversation.tasks.open")}>
                  <Icon s={x.status} />
                  <span className="min-w-0 flex-1 truncate font-medium">{x.desc || x.id}</span>
                  <span className="shrink-0 text-xs text-muted-foreground">{t(`conversation.tasks.status.${x.status}`, { defaultValue: x.status })}</span>
                  {x.start && <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{ago(Date.parse(x.start) / 1000)}</span>}
                </button>
              </li>
            ))}
          </ul>
          </ScrollArea>
        </DialogContent>
      </Dialog>
      <Dialog open={!!pick} onOpenChange={(o) => !o && setPick(null)}>
        <DialogContent className="flex h-[85vh] max-h-[85vh] min-w-0 flex-col gap-2 sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle className="truncate">{cur?.desc || pick?.id}</DialogTitle>
            <DialogDescription>{cur?.summary || t(`conversation.tasks.status.${cur?.status ?? "running"}`, { defaultValue: cur?.status })}</DialogDescription>
          </DialogHeader>
          <ScrollArea className="min-h-0 flex-1 rounded-md border bg-muted/30">
            <pre className="p-3 font-mono text-xs break-all whitespace-pre-wrap">{out?.id === pick?.id ? (out?.ok ? out.text || t("conversation.tasks.empty") : t("conversation.tasks.noOutput")) : "…"}</pre>
          </ScrollArea>
        </DialogContent>
      </Dialog>
    </>
  )
}
