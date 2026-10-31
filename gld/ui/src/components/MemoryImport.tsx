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
const SELECT_CLASS_NAME = "rounded-md border bg-background px-2 py-1 text-sm"
const MAX_PACK_SIZE_BYTES = 32 << 20
const DEFAULT_ACT: Act = "skip"

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

  const existingMemoryNames = (dir: string) => {
    const project = list.projects.find((candidate) => candidate.dir === dir)
    const memoryFiles = project?.files.filter((file) => file.key.startsWith(`m/${dir}/`))
    return new Set(memoryFiles?.map((file) => file.name))
  }
  const clash = (entry: PackEntry) => !!dest[entry.project] && existingMemoryNames(dest[entry.project]).has(entry.name)
  const actFor = (entryIndex: number) => acts[entryIndex] ?? DEFAULT_ACT
  const willImport = (entry: PackEntry, entryIndex: number) => {
    if (!dest[entry.project]) return false
    if (off.has(entryIndex)) return false
    return !clash(entry) || actFor(entryIndex) !== "skip"
  }
  const todo = (entries ?? []).filter(willImport)

  const close = () => {
    setEntries(null)
    setError("")
    setDest({})
    setOff(new Set())
    setActs({})
    onClose()
  }
  const load = async (packFile: File | undefined) => {
    if (!packFile) return
    try {
      if (packFile.size > MAX_PACK_SIZE_BYTES) throw new Error(t("memory.pack.tooBig"))
      setEntries(parsePack(await packFile.text()))
      setError("")
    } catch (loadError) {
      setEntries(null)
      setError(errMsg(loadError))
    }
  }
  const run = async () => {
    if (!entries) return
    setBusy(true)
    const takenNamesByDir = new Map<string, Set<string>>()
    let importedCount = 0
    const failures: string[] = []
    for (const [entryIndex, entry] of entries.entries()) {
      const dir = dest[entry.project]
      if (!dir || off.has(entryIndex)) continue
      const usedNames = takenNamesByDir.get(dir) ?? existingMemoryNames(dir)
      takenNamesByDir.set(dir, usedNames)
      let name = entry.name
      if (usedNames.has(name)) {
        const act = actFor(entryIndex)
        if (act === "skip") continue
        if (act === "both") name = freeName(name, usedNames)
      }
      try {
        await api.memorySave(`m/${dir}/${name}`, entry.text, true)
        usedNames.add(name)
        importedCount++
      } catch (saveError) {
        failures.push(`${name} : ${errMsg(saveError)}`)
      }
    }
    setBusy(false)
    if (failures.length) toast.error(t("memory.pack.partial", { ok: importedCount, failed: failures.length, first: failures[0] }))
    else toast.success(t("memory.pack.imported", { count: importedCount }))
    onDone()
    close()
  }

  const setEntryChecked = (entryIndex: number, checked: unknown) =>
    setOff((uncheckedEntries) => {
      if (checked) {
        uncheckedEntries.delete(entryIndex)
        return new Set(uncheckedEntries)
      }
      return new Set(uncheckedEntries).add(entryIndex)
    })

  const packProjects = [...new Set((entries ?? []).map((entry) => entry.project))]
  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && close()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("memory.pack.dialogTitle")}</DialogTitle>
          <DialogDescription>{t("memory.pack.dialogDesc")}</DialogDescription>
        </DialogHeader>
        <input type="file" accept=".json,application/json" onChange={(event) => load(event.target.files?.[0])} aria-label={t("memory.pack.fileAria")} className="text-sm file:mr-3 file:rounded-md file:border file:bg-muted file:px-3 file:py-1" />
        {error && <p className="text-sm text-destructive">{error}</p>}
        {entries && (
          <ScrollArea className="max-h-[50vh]">
            <div className="space-y-4 pr-3">
              {packProjects.map((packProject) => (
                <section key={packProject}>
                  <div className="mb-1 flex flex-wrap items-center gap-2 text-sm">
                    <span className="font-medium">{packProject || t("memory.pack.noProject")}</span>
                    <select aria-label={t("memory.pack.destAria", { name: packProject })} value={dest[packProject] ?? ""} onChange={(event) => setDest({ ...dest, [packProject]: event.target.value })} className={SELECT_CLASS_NAME}>
                      <option value="">{t("memory.pack.ignore")}</option>
                      {list.targets.map((target) => (
                        <option key={target.dir} value={target.dir}>
                          {tilde(target.project, homeDir)}
                        </option>
                      ))}
                    </select>
                  </div>
                  <ul className="space-y-1">
                    {entries.flatMap((entry, entryIndex) => {
                      if (entry.project !== packProject) return []
                      const isChecked = !off.has(entryIndex)
                      return [
                        <li key={entryIndex} className="flex flex-wrap items-center gap-2 text-sm">
                          <Checkbox checked={isChecked} disabled={!dest[packProject]} aria-label={entry.name} onCheckedChange={(checked) => setEntryChecked(entryIndex, checked)} />
                          <span className="font-mono text-xs">{entry.name}</span>
                          {clash(entry) && isChecked && (
                            <select aria-label={t("memory.pack.clashAria", { name: entry.name })} value={actFor(entryIndex)} onChange={(event) => setActs({ ...acts, [entryIndex]: event.target.value as Act })} className={SELECT_CLASS_NAME}>
                              <option value="skip">{t("memory.pack.skip")}</option>
                              <option value="replace">{t("memory.pack.replace")}</option>
                              <option value="both">{t("memory.pack.both")}</option>
                            </select>
                          )}
                        </li>,
                      ]
                    })}
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
