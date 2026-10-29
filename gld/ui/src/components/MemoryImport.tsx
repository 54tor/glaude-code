import { useState } from "react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { tilde } from "@/lib/format"
import { freeName, parsePack, type PackEntry } from "@/lib/memoryPack"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import type { List } from "./memoryLib"

type Act = "skip" | "replace" | "both"
const selectCls = "rounded-md border bg-background px-2 py-1 text-sm"

// Import d'un paquet de mémoires : le destinataire désigne le projet cible de chaque projet du paquet,
// coche les entrées voulues et tranche chaque conflit de nom.
export function MemoryImport({ open, list, homeDir, onClose, onDone }: { open: boolean; list: List; homeDir: string; onClose: () => void; onDone: () => void }) {
  const { t } = useTranslation()
  const [entries, setEntries] = useState<PackEntry[] | null>(null)
  const [error, setError] = useState("")
  const [dest, setDest] = useState<Record<string, string>>({}) // projet du paquet → dossier local ("" : ignorer)
  const [off, setOff] = useState<Set<number>>(new Set()) // entrées décochées
  const [acts, setActs] = useState<Record<number, Act>>({})
  const [busy, setBusy] = useState(false)

  const names = (dir: string) => new Set(list.projects.find((p) => p.dir === dir)?.files.filter((f) => f.key.startsWith(`m/${dir}/`)).map((f) => f.name))
  const clash = (e: PackEntry) => !!dest[e.project] && names(dest[e.project]).has(e.name)
  const todo = (entries ?? []).filter((e, i) => dest[e.project] && !off.has(i) && (!clash(e) || (acts[i] ?? "skip") !== "skip"))

  const close = () => {
    setEntries(null)
    setError("")
    setDest({})
    setOff(new Set())
    setActs({})
    onClose()
  }
  const load = async (f: File | undefined) => {
    if (!f) return
    try {
      if (f.size > 32 << 20) throw new Error(t("memory.pack.tooBig"))
      setEntries(parsePack(await f.text()))
      setError("")
    } catch (e) {
      setEntries(null)
      setError(errMsg(e))
    }
  }
  const run = async () => {
    if (!entries) return
    setBusy(true)
    const taken = new Map<string, Set<string>>()
    let ok = 0
    const fails: string[] = []
    for (const [i, e] of entries.entries()) {
      const dir = dest[e.project]
      if (!dir || off.has(i)) continue
      const used = taken.get(dir) ?? names(dir)
      taken.set(dir, used)
      let name = e.name
      if (used.has(name)) {
        const a = acts[i] ?? "skip"
        if (a === "skip") continue
        if (a === "both") name = freeName(name, used)
      }
      try {
        await api.memorySave(`m/${dir}/${name}`, e.text, true)
        used.add(name)
        ok++
      } catch (err) {
        fails.push(`${name} : ${errMsg(err)}`)
      }
    }
    setBusy(false)
    if (fails.length) toast.error(t("memory.pack.partial", { ok, failed: fails.length, first: fails[0] }))
    else toast.success(t("memory.pack.imported", { count: ok }))
    onDone()
    close()
  }

  const groups = [...new Set((entries ?? []).map((e) => e.project))]
  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("memory.pack.dialogTitle")}</DialogTitle>
          <DialogDescription>{t("memory.pack.dialogDesc")}</DialogDescription>
        </DialogHeader>
        <input type="file" accept=".json,application/json" onChange={(e) => load(e.target.files?.[0])} aria-label={t("memory.pack.fileAria")} className="text-sm file:mr-3 file:rounded-md file:border file:bg-muted file:px-3 file:py-1" />
        {error && <p className="text-sm text-destructive">{error}</p>}
        {entries && (
          <ScrollArea className="max-h-[50vh]">
            <div className="space-y-4 pr-3">
              {groups.map((g) => (
                <section key={g}>
                  <div className="mb-1 flex flex-wrap items-center gap-2 text-sm">
                    <span className="font-medium">{g || t("memory.pack.noProject")}</span>
                    <select aria-label={t("memory.pack.destAria", { name: g })} value={dest[g] ?? ""} onChange={(e) => setDest({ ...dest, [g]: e.target.value })} className={selectCls}>
                      <option value="">{t("memory.pack.ignore")}</option>
                      {list.targets.map((x) => (
                        <option key={x.dir} value={x.dir}>
                          {tilde(x.project, homeDir)}
                        </option>
                      ))}
                    </select>
                  </div>
                  <ul className="space-y-1">
                    {entries.flatMap((e, i) =>
                      e.project !== g
                        ? []
                        : [
                            <li key={i} className="flex flex-wrap items-center gap-2 text-sm">
                              <Checkbox checked={!off.has(i)} disabled={!dest[g]} aria-label={e.name} onCheckedChange={(c) => setOff((s) => (c ? (s.delete(i), new Set(s)) : new Set(s).add(i)))} />
                              <span className="font-mono text-xs">{e.name}</span>
                              {clash(e) && !off.has(i) && (
                                <select aria-label={t("memory.pack.clashAria", { name: e.name })} value={acts[i] ?? "skip"} onChange={(ev) => setActs({ ...acts, [i]: ev.target.value as Act })} className={selectCls}>
                                  <option value="skip">{t("memory.pack.skip")}</option>
                                  <option value="replace">{t("memory.pack.replace")}</option>
                                  <option value="both">{t("memory.pack.both")}</option>
                                </select>
                              )}
                            </li>,
                          ]
                    )}
                  </ul>
                </section>
              ))}
            </div>
          </ScrollArea>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={close}>
            {t("common.cancel")}
          </Button>
          <Button onClick={run} disabled={busy || todo.length === 0}>
            {t("memory.pack.run", { count: todo.length })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
