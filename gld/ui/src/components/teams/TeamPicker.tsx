import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { sameLabel } from "./editorBits"
import {
  teamsCompose,
  type Catalog,
  type Composition,
  type Run,
  type Team,
} from "@/lib/teams"

type Launched = { ok: boolean; out?: string; run?: Run }
const MAX_MATES = 5 // même plafond que maxMates côté Go

export function TeamPicker({
  catalog,
  onLaunch,
  pickDir,
  home = "",
  onEdit,
}: {
  catalog: Catalog
  onLaunch: (ids: string[], goal: string, dir: string) => Promise<Launched>
  pickDir: (title: string, start: string) => Promise<string | null>
  home?: string
  onEdit?: (tm: Team) => void
}) {
  const { t } = useTranslation()
  const [sel, setSel] = useState<string[]>([])
  const [compo, setCompo] = useState<Composition | null>(null)
  const [goal, setGoal] = useState("")
  const [dir, setDir] = useState("")
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState("")
  const [conflicts, setConflicts] = useState<string[]>([])
  const seq = useRef(0)

  const names = new Map(catalog.agents.map((a) => [a.ID, a.Name]))
  const toggle = (id: string) =>
    setSel((s) => (s.includes(id) ? s.filter((x) => x !== id) : [...s, id]))

  // une team supprimée ou renommée ailleurs ne reste pas sélectionnée
  useEffect(
    () =>
      setSel((s) =>
        s.every((id) => catalog.teams.some((t) => t.ID === id))
          ? s
          : s.filter((id) => catalog.teams.some((t) => t.ID === id))
      ),
    [catalog.teams]
  )
  useEffect(() => {
    const n = ++seq.current // une réponse lente d'une sélection précédente est ignorée
    if (!sel.length) return void setCompo(null)
    void teamsCompose(sel).then((r) => {
      if (n === seq.current) setCompo(r.ok ? r : null)
    })
  }, [sel])

  const launch = async () => {
    setBusy(true)
    setErr("")
    setConflicts([])
    const r = await onLaunch(sel, goal.trim(), dir)
    setBusy(false)
    if (!r.ok) setErr(r.out ?? "")
    setConflicts(r.run?.Conflicts ?? [])
  }
  const ready = sel.length > 0 && goal.trim() !== "" && dir !== "" && !busy

  return (
    <div className="space-y-6">
      <section>
        <h2 className="mb-2 text-sm font-semibold">{t("teams.title")}</h2>
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
          {catalog.teams.map((tm) => {
            const on = sel.includes(tm.ID)
            return (
              <div key={tm.ID} className="relative">
                <div
                  role="checkbox"
                  aria-checked={on}
                  tabIndex={0}
                  onClick={() => toggle(tm.ID)}
                  onKeyDown={(e) => {
                    if (e.key === " " || e.key === "Enter") {
                      e.preventDefault()
                      toggle(tm.ID)
                    }
                  }}
                  className={`cursor-pointer rounded-lg border p-3 pr-20 outline-none focus-visible:ring-3 focus-visible:ring-ring/50 ${on ? "border-primary bg-primary/10" : "border-border hover:bg-muted"}`}
                >
                  <div className="text-sm font-medium">{tm.Name}</div>
                  <div className="text-xs text-muted-foreground">
                    {tm.Members.map((m) => names.get(m) ?? m).join(" · ")}
                  </div>
                </div>
                {onEdit && (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="absolute top-2 right-2"
                    aria-label={t("teams.edit.edit", { name: tm.Name })}
                    onClick={() => onEdit(tm)}
                  >
                    {t("teams.edit.modify")}
                  </Button>
                )}
              </div>
            )
          })}
        </div>
      </section>

      <section className="rounded-lg border p-3">
        <h2 className="mb-2 text-sm font-semibold">{t("teams.composition")}</h2>
        <p className="mb-2 text-sm text-muted-foreground">
          {t("teams.count", {
            teams: sel.length,
            mates: compo?.mates.length ?? 0,
          })}
        </p>
        {compo && (
          <ul className="space-y-1 text-sm">
            <li>
              <b>{compo.lead.Name}</b>{" "}
              <span className="text-muted-foreground">
                · {t("teams.lead")}
                {!sameLabel(compo.lead.Name, compo.lead.Role) &&
                  ` · ${compo.lead.Role}`}
              </span>
            </li>
            {compo.mates.map((a) => (
              <li key={a.ID}>
                <b>{a.Name}</b>{" "}
                {!sameLabel(a.Name, a.Role) && (
                  <span className="text-muted-foreground">· {a.Role}</span>
                )}
              </li>
            ))}
          </ul>
        )}
        {compo?.tooMany && (
          <p className="mt-2 text-sm text-destructive">
            {t("teams.tooMany", { max: MAX_MATES })}
          </p>
        )}
      </section>

      <section className="space-y-3">
        <textarea
          value={goal}
          onChange={(e) => setGoal(e.target.value)}
          placeholder={t("teams.goal")}
          aria-label={t("teams.goal")}
          rows={4}
          className="w-full rounded-lg border bg-transparent p-2 text-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        />
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            onClick={async () =>
              setDir((await pickDir(t("teams.pickDir"), dir || home)) ?? dir)
            }
          >
            {t("teams.choose")}
          </Button>
          <span className="truncate text-sm text-muted-foreground" title={dir}>
            {dir || t("teams.dir")}
          </span>
        </div>
        <Button disabled={!ready} onClick={launch}>
          {t("teams.launch")}
        </Button>
        {err && <p className="text-sm text-destructive">{err}</p>}
        {conflicts.length > 0 && (
          <div className="text-sm">
            <p className="font-medium">{t("teams.conflicts")}</p>
            <ul className="text-muted-foreground">
              {conflicts.map((c) => (
                <li key={c}>{c}</li>
              ))}
            </ul>
          </div>
        )}
      </section>
    </div>
  )
}
