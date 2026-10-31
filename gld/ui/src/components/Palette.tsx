import { asked } from "@/lib/sessionsView"
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
type ConfigBundle = { settings?: { field?: Record<string, string> } }

export function Palette({ open, onOpenChange, rows, home, go, read }: { open: boolean; onOpenChange: (isOpen: boolean) => void; rows: Row[]; home: string; go: (route: Route) => void; read: (session: { id: string }) => void }) {
  const { t } = useTranslation()
  const [skills, setSkills] = useState<Skill[]>([])
  useEffect(() => {
    if (open && !skills.length) api.skills().then((response) => setSkills(response.skills), () => {})
  }, [open, skills.length])
  const pick = (action: () => void) => () => {
    onOpenChange(false)
    action()
  }
  const sessions = [...rows].sort((left, right) => asked(right.s) - asked(left.s)).slice(0, MAX_SESSIONS)
  const configBundle = i18n.getResourceBundle(i18n.language, "config") as ConfigBundle | undefined
  const fields = configBundle?.settings?.field ?? {}
  return (
    <CommandDialog open={open} onOpenChange={onOpenChange} title={t("shell.palette.title")} description={t("shell.palette.description")}>
      <CommandInput placeholder={t("shell.palette.placeholder")} />
      <CommandList>
        <CommandEmpty>{t("shell.palette.empty")}</CommandEmpty>
        <CommandGroup heading={t("shell.palette.pages")}>
          {PAGES.map(([pageRoute, titleKey]) => (
            <CommandItem key={titleKey} value={"page " + t(titleKey)} onSelect={pick(() => go(pageRoute))}>
              <FileText /> {t(titleKey)}
            </CommandItem>
          ))}
        </CommandGroup>
        <CommandGroup heading={t("shell.palette.sessions")}>
          {sessions.map((row) => (
            <CommandItem key={row.s.id} value={"session " + row.s.title + " " + row.g.project} onSelect={pick(() => read(row.s))}>
              <MessageSquare />
              <span className="min-w-0 flex-1 truncate">{row.s.title}</span>
              <span className="shrink-0 text-xs text-muted-foreground">{tilde(row.g.project, home)}</span>
            </CommandItem>
          ))}
        </CommandGroup>
        <CommandGroup heading={t("shell.palette.skills")}>
          {skills.map((skill) => (
            <CommandItem key={skill.key} value={"skill " + skill.name + " " + skill.description} onSelect={pick(() => go({ kind: "skills", key: skill.key }))}>
              <BookOpen />
              <span className="min-w-0 flex-1 truncate">{skill.name}</span>
              <span className="shrink-0 text-xs text-muted-foreground">{skill.plugin ?? (skill.project ? tilde(skill.project, home) : "")}</span>
            </CommandItem>
          ))}
        </CommandGroup>
        <CommandGroup heading={t("shell.palette.settings")}>
          {Object.entries(fields).map(([fieldKey, label]) => (
            <CommandItem key={fieldKey} value={"setting " + label + " " + fieldKey} onSelect={pick(() => go({ kind: "config", tab: "reglages" }))}>
              <Settings2 />
              <span className="min-w-0 flex-1 truncate">{label}</span>
              <code className="shrink-0 text-xs text-muted-foreground">{fieldKey}</code>
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  )
}
