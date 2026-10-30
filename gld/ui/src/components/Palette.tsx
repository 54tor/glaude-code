import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { BookOpen, FileText, MessageSquare, Settings2 } from "lucide-react"
import { i18n } from "@/i18n"
import { api, type Skill } from "@/lib/api"
import { tilde } from "@/lib/format"
import type { Route } from "@/lib/route"
import { CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command"
import type { Row } from "./actions"

const PAGES: [Route, string][] = [
  [{ kind: "recent" }, "shell.app.title.recent"],
  [{ kind: "all" }, "shell.app.title.all"],
  [{ kind: "active" }, "shell.app.title.active"],
  [{ kind: "snippets", path: "" }, "shell.app.title.snippets"],
  [{ kind: "memory", key: "" }, "shell.app.title.memory"],
  [{ kind: "config", tab: "" }, "shell.app.title.config"],
  [{ kind: "extensions" }, "shell.app.title.extensions"],
  [{ kind: "teams" }, "teams.title"],
  [{ kind: "tabs" }, "shell.app.title.tabs"],
]
const MAX_SESSIONS = 300 // ponytail: cmdk filtre côté client ; au-delà, les plus anciennes sortent de la palette

// Palette Ctrl+K : pages, sessions, skills et réglages dans une seule liste filtrée.
export function Palette({ open, onOpenChange, rows, home, go, read }: { open: boolean; onOpenChange: (o: boolean) => void; rows: Row[]; home: string; go: (r: Route) => void; read: (s: { id: string }) => void }) {
  const { t } = useTranslation()
  const [skills, setSkills] = useState<Skill[]>([])
  useEffect(() => {
    if (open && !skills.length) api.skills().then((r) => setSkills(r.skills), () => {})
  }, [open, skills.length])
  const pick = (f: () => void) => () => (onOpenChange(false), f())
  const sessions = [...rows].sort((a, b) => b.s.mtime - a.s.mtime).slice(0, MAX_SESSIONS)
  const fields = (i18n.getResourceBundle(i18n.language, "config") as { settings?: { field?: Record<string, string> } } | undefined)?.settings?.field ?? {}
  return (
    <CommandDialog open={open} onOpenChange={onOpenChange} title={t("shell.palette.title")} description={t("shell.palette.description")}>
      <CommandInput placeholder={t("shell.palette.placeholder")} />
      <CommandList>
        <CommandEmpty>{t("shell.palette.empty")}</CommandEmpty>
        <CommandGroup heading={t("shell.palette.pages")}>
          {PAGES.map(([r, k]) => (
            <CommandItem key={k} value={"page " + t(k)} onSelect={pick(() => go(r))}>
              <FileText /> {t(k)}
            </CommandItem>
          ))}
        </CommandGroup>
        <CommandGroup heading={t("shell.palette.sessions")}>
          {sessions.map((r) => (
            <CommandItem key={r.s.id} value={"session " + r.s.title + " " + r.g.project} onSelect={pick(() => read(r.s))}>
              <MessageSquare />
              <span className="min-w-0 flex-1 truncate">{r.s.title}</span>
              <span className="shrink-0 text-xs text-muted-foreground">{tilde(r.g.project, home)}</span>
            </CommandItem>
          ))}
        </CommandGroup>
        <CommandGroup heading={t("shell.palette.skills")}>
          {skills.map((s) => (
            <CommandItem key={s.key} value={"skill " + s.name + " " + s.description} onSelect={pick(() => go({ kind: "skills", key: s.key }))}>
              <BookOpen />
              <span className="min-w-0 flex-1 truncate">{s.name}</span>
              <span className="shrink-0 text-xs text-muted-foreground">{s.plugin ?? (s.project ? tilde(s.project, home) : "")}</span>
            </CommandItem>
          ))}
        </CommandGroup>
        <CommandGroup heading={t("shell.palette.settings")}>
          {Object.entries(fields).map(([k, label]) => (
            <CommandItem key={k} value={"setting " + label + " " + k} onSelect={pick(() => go({ kind: "config", tab: "reglages" }))}>
              <Settings2 />
              <span className="min-w-0 flex-1 truncate">{label}</span>
              <code className="shrink-0 text-xs text-muted-foreground">{k}</code>
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  )
}
