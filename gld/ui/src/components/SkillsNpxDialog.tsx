import { useTranslation } from "react-i18next"
import { useEffect, useRef, useState } from "react"
import { Download, Fan, Search } from "lucide-react"
import { api, type SkillHit } from "@/lib/api"
import { tilde } from "@/lib/format"
import { reportAction } from "@/lib/report"
import { errMsg } from "@/lib/apiError"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Choose } from "./Choose"

const SKILLS_PKG = "skills@1.7.4" // la même version épinglée que le serveur (skills_npx.go)

// La commande que le serveur lancera, telle qu'elle s'affiche dans la confirmation.
export function npxCommand(
  action: "add" | "update" | "remove",
  source: string,
  skill: string,
  global: boolean
) {
  const g = global ? " -g" : ""
  if (action === "add")
    return `npx --yes ${SKILLS_PKG} add ${source} -s ${skill} -a claude-code -y${g}`
  return `npx --yes ${SKILLS_PKG} ${action} ${skill}${action === "remove" ? " -a claude-code" : ""} -y${global ? " -g" : " -p"}`
}

// scope : "user" ou "p/<dossier>" (la clé du projet).
export async function npxAct(
  action: "add" | "update" | "remove",
  source: string,
  skill: string,
  scope: string
) {
  const project = scope.startsWith("p/") ? scope.slice(2) : ""
  return reportAction(() =>
    api.skillNpx({
      Action: action,
      Source: source,
      Skill: skill,
      Scope: project ? "project" : "user",
      Project: project,
    })
  )
}

type Project = { dir: string; project: string }

// Recherche (npx skills find) et installation (npx skills add) : rien ne part sans la confirmation de la commande exacte.
export function SkillsNpxDialog({
  open,
  onClose,
  projects,
  home,
  onChange,
}: {
  open: boolean
  onClose: () => void
  projects: Project[]
  home: string
  onChange: () => void
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex h-[80vh] max-h-[80vh] flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("skills.npx.title")}</DialogTitle>
          <DialogDescription>{t("skills.npx.description")}</DialogDescription>
        </DialogHeader>
        <ScrollArea className="min-h-0 flex-1">
          <SkillsNpxPanel projects={projects} home={home} onChange={onChange} />
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}

// fixed : portée imposée par la page ("user" ou "p/<dossier>") ; sinon elle se choisit à la confirmation.
export function SkillsNpxPanel({
  projects,
  home,
  onChange,
  fixed,
  query,
}: {
  projects: Project[]
  home: string
  onChange: () => void
  fixed?: string
  query?: string // recherche portée par le parent : lancée après une pause de frappe, sans champ ni bouton ici
}) {
  const { t } = useTranslation()
  const [own, setQ] = useState("")
  const q = query ?? own
  const [hits, setHits] = useState<SkillHit[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [pick, setPick] = useState<SkillHit | null>(null)
  const [chosen, setScope] = useState("user")
  const [err, setErr] = useState("")
  const scope = fixed ?? chosen
  const scopes = [
    ["user", t("skills.npx.scopeUser")],
    ...projects.map(
      (p) =>
        [
          "p/" + p.dir,
          t("skills.npx.scopeProject", { name: tilde(p.project, home) }),
        ] as const
    ),
  ] as const

  const seq = useRef(0) // seule la dernière recherche s'affiche
  const search = async () => {
    if (!q.trim()) return
    const n = ++seq.current
    setBusy(true)
    setErr("")
    const r = await api
      .skillsFind(q.trim())
      .catch(
        (e) =>
          void (
            n === seq.current &&
            setErr(errMsg(e) || t("skills.npx.searchFailed"))
          )
      )
    if (n !== seq.current) return
    setBusy(false)
    if (r) setHits(r.hits ?? [])
  }
  useEffect(() => {
    if (query === undefined) return
    const h = setTimeout(
      () => (query.trim().length < 2 ? setHits(null) : search()),
      500
    )
    return () => clearTimeout(h)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query])
  const install = async () => {
    if (!pick || busy) return
    setBusy(true)
    const ok = await npxAct("add", pick.source, pick.skill, scope)
    setBusy(false)
    if (ok) {
      setPick(null)
      onChange()
    }
  }

  if (pick)
    return (
      <div className="flex flex-col gap-3 rounded-md border p-3">
        <h3 className="text-sm font-semibold">
          {t("skills.npx.confirmTitle", { skill: pick.skill })}
        </h3>
        <p className="text-sm text-muted-foreground">
          {t("skills.npx.confirmBody")}
        </p>
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">
            {t("skills.npx.scope")}
          </span>
          {fixed !== undefined ? (
            <span className="text-sm font-medium">
              {scopes.find(([k]) => k === fixed)?.[1]}
            </span>
          ) : (
            <Choose
              value={scope}
              onChange={setScope}
              options={scopes}
              label={t("skills.npx.scope")}
              className="min-w-0"
            />
          )}
        </div>
        <div>
          <div className="mb-1 text-xs text-muted-foreground">
            {t("skills.npx.command")}
          </div>
          <code className="block rounded-md bg-muted px-2 py-1.5 text-xs break-all">
            {npxCommand("add", pick.source, pick.skill, scope === "user")}
          </code>
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={() => setPick(null)} disabled={busy}>
            {t("skills.npx.back")}
          </Button>
          <Button onClick={install} disabled={busy}>
            {busy ? <Fan className="animate-spin" /> : <Download />}{" "}
            {busy ? t("skills.npx.running") : t("skills.npx.confirmInstall")}
          </Button>
        </div>
      </div>
    )
  return (
    <div className="space-y-2">
      {query === undefined && (
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            search()
          }}
        >
          <Input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={t("skills.npx.search")}
            aria-label={t("skills.npx.search")}
            maxLength={80}
          />
          <Button type="submit" variant="outline" disabled={busy || !q.trim()}>
            {busy ? <Fan className="animate-spin" /> : <Search />}{" "}
            {t("skills.npx.searchBtn")}
          </Button>
        </form>
      )}
      {query !== undefined && busy && (
        <Fan className="size-4 animate-spin text-muted-foreground" />
      )}
      {err && <p className="text-sm text-destructive">{err}</p>}
      {!!hits?.length && (
        <ul className="divide-y rounded-md border">
          {hits.map((h) => (
            <li key={h.ref} className="flex items-center gap-2 px-3 py-1.5">
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium">{h.ref}</div>
                <div className="text-xs text-muted-foreground">
                  {t("skills.npx.installs", { n: h.installs })}
                </div>
              </div>
              <Button size="xs" variant="outline" onClick={() => setPick(h)}>
                <Download /> {t("skills.npx.install")}
              </Button>
            </li>
          ))}
        </ul>
      )}
      {hits?.length === 0 && (
        <p className="rounded-md border px-3 py-6 text-center text-sm text-muted-foreground">
          {t("skills.npx.none")}
        </p>
      )}
    </div>
  )
}
