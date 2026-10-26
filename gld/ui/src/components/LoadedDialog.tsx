import { useEffect, useState } from "react"
import { Trans, useTranslation } from "react-i18next"
import { api, type LoadEvent } from "@/lib/api"
import { ago, tilde } from "@/lib/format"
import { FileText } from "lucide-react"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

const REASONS: Record<string, string> = {
  session_start: "conversation.loaded.reasons.sessionStart",
  nested_traversal: "conversation.loaded.reasons.nested",
  path_glob_match: "conversation.loaded.reasons.glob",
  include: "conversation.loaded.reasons.include",
  compact: "conversation.loaded.reasons.compact",
}

export const HOOK_SNIPPET = `"hooks": {
  "InstructionsLoaded": [
    { "hooks": [{ "type": "command", "command": "[ -x \\"$HOME/.local/bin/gld\\" ] && \\"$HOME/.local/bin/gld\\" _loaded; exit 0" }] }
  ]
}`

// Les fichiers d'instructions (CLAUDE.md, règles, imports) que Claude a réellement chargés dans une session, d'après le
// hook InstructionsLoaded. Sans hook installé, ou pour une session plus ancienne que lui, la liste est vide.
export function LoadedDialog({ id, home, open, onClose }: { id: string; home: string; open: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  const [files, setFiles] = useState<LoadEvent[] | null>(null)
  const [view, setView] = useState<{ file: string; text: string | null } | null>(null) // fichier ouvert dans une fenêtre au-dessus
  const openFile = (file: string) => {
    setView({ file, text: null })
    api.sessionLoadedFile(id, file).then((r) => setView((v) => (v?.file === file ? { file, text: r.text ?? "" } : v)), () => setView((v) => (v?.file === file ? { file, text: "" } : v)))
  }
  useEffect(() => {
    if (!open || !id) return
    let live = true
    api.sessionLoaded(id).then((r) => live && setFiles(r.files))
    return () => {
      live = false
    }
  }, [open, id])
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] min-w-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("conversation.loaded.title")}</DialogTitle>
          <DialogDescription>{t("conversation.loaded.description")}</DialogDescription>
        </DialogHeader>
        {files === null ? null : files.length === 0 ? (
          <div className="min-h-0 min-w-0 space-y-2 overflow-y-auto text-sm">
            <p>{t("conversation.loaded.empty")}</p>
            <p className="text-muted-foreground">
              <Trans i18nKey="conversation.loaded.enable" components={[<code key="0" />, <code key="1" />, <code key="2" />, <code key="3" />]} />
            </p>
            <pre className="max-w-full overflow-x-auto rounded-lg border bg-muted/40 p-2 text-xs">{HOOK_SNIPPET}</pre>
          </div>
        ) : (
          <ScrollArea className="min-h-0 min-w-0">
            <ul className="space-y-1 pr-3 text-sm">
              {files.map((f, i) => (
                <li key={i} className="rounded-md px-2 py-1 hover:bg-muted">
                  <button type="button" onClick={() => openFile(f.file)} title={t("conversation.loaded.openFile")} className="flex cursor-pointer items-start gap-1.5 text-left break-all hover:underline">
                    <FileText className="mt-0.5 size-3.5 shrink-0" /> {tilde(f.file, home)}
                  </button>
                  <span className="block text-xs text-muted-foreground">
                    {REASONS[f.reason ?? ""] ? t(REASONS[f.reason ?? ""]) : f.reason}
                    {f.globs?.length ? ` · ${f.globs.join(", ")}` : ""}
                    {f.trigger ? ` · ${t("conversation.loaded.for", { path: tilde(f.trigger, home) })}` : ""} · {ago(f.at)}
                  </span>
                </li>
              ))}
            </ul>
          </ScrollArea>
        )}
      </DialogContent>
      <Dialog open={!!view} onOpenChange={(o) => !o && setView(null)}>
        <DialogContent className="flex h-[85vh] max-h-[85vh] min-w-0 flex-col overflow-hidden sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle className="text-sm break-all">{view && tilde(view.file, home)}</DialogTitle>
            <DialogDescription className="sr-only">{t("conversation.loaded.fileDescription")}</DialogDescription>
          </DialogHeader>
          <ScrollArea className="min-h-0 flex-1 rounded-lg border bg-muted/40">
            <pre className="p-3 text-xs break-words whitespace-pre-wrap">{view?.text ?? "…"}</pre>
          </ScrollArea>
        </DialogContent>
      </Dialog>
    </Dialog>
  )
}
