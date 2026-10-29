import { useState, type ReactNode } from "react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { i18n } from "@/i18n"
import { api, type CfgEdit, type CfgIssue, type CfgReply } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { DiffView, lineDiff } from "./MemoryDiff"

// Après une écriture : ce que les sessions ouvertes en voient. Claude Code surveille ses fichiers de réglages et les recharge
// seul (permissions, hooks, variables d'environnement…) : rien à pousser. Quelques clés ne sont lues qu'au démarrage d'une
// session ou passent par une commande : on dit lesquelles, d'après les lignes qui ont changé.
export function liveNotice(before: string, after: string, sessions: number): string {
  const changed = lineDiff(before, after)
    .filter((r) => r.op !== " ")
    .map((r) => r.t)
    .join("\n")
  const todo: string[] = []
  if (/"model"\s*:/.test(changed)) todo.push(i18n.t("config.edit.model"))
  if (/"(effortLevel|modelSettings)"\s*:/.test(changed)) todo.push(i18n.t("config.edit.effort"))
  if (/"enabledPlugins"|"pluginConfigs"|"[\w.-]+@[\w.-]+"\s*:/.test(changed)) todo.push(i18n.t("config.edit.plugins"))
  const live = sessions ? i18n.t("config.edit.live", { count: sessions }) : i18n.t("config.edit.liveNone")
  return todo.length ? i18n.t("config.edit.manual", { live, todo: todo.join(i18n.t("config.edit.sep")) }) : live
}

type Preview = { then?: () => void; edit?: CfgEdit; raw?: { file: string; text: string }; perm?: { file: string; text: string; mode: string }; summary?: string; before: string; after: string; warnings: CfgIssue[] }

// Toute modification d'un fichier de réglages passe par ici : aperçu des différences, confirmation, puis écriture
// (le serveur fait une sauvegarde avant). Renvoie propose() et le dialogue à afficher.
export function useConfigEdit(onDone: () => void | Promise<unknown>, sessions = 0): { gone: (file: string, kind: string, rule: string) => boolean; added: (file: string, kind: string) => string[]; moveRule: (file: string, from: string, rule: string, to: string, text: string) => Promise<void>; propose: (e: CfgEdit) => Promise<void>; proposeRaw: (file: string, text: string) => Promise<string | null>; proposePerms: (file: string, text: string, mode: string, then?: () => void, now?: boolean) => Promise<string | null>; dialog: ReactNode } {
  const { t } = useTranslation()
  const [pv, setPv] = useState<Preview | null>(null)
  const [busy, setBusy] = useState(false)
  // Modification simple (une règle, un réglage, une variable) : appliquée tout de suite, sans fenêtre de confirmation. Le serveur
  // fait une sauvegarde avant d'écrire (onglet Sauvegardes) ; le toast dit ce que les sessions ouvertes en voient.
  // Retrait d'une règle : la ligne disparaît tout de suite (gone) et ne revient qu'en cas d'échec.
  const [hidden, setHidden] = useState<Set<string>>(new Set())
  const hide = (k: string, on: boolean) =>
    setHidden((s) => {
      const n = new Set(s)
      if (on) n.add(k)
      else n.delete(k)
      return n
    })
  const ruleKey = (file: string, kind: string, rule: string) => JSON.stringify([file, kind, rule])
  const gone = (file: string, kind: string, rule: string) => hidden.has(ruleKey(file, kind, rule))
  // Règles ajoutées en attente d'écriture : affichées tout de suite.
  const [pending, setPending] = useState<string[]>([])
  const added = (file: string, kind: string) => pending.filter((k) => (JSON.parse(k) as string[])[0] === file && (JSON.parse(k) as string[])[1] === kind).map((k) => (JSON.parse(k) as string[])[2])
  // Changer une règle de catégorie ou de texte : elle passe d'un coup de l'ancienne place à la nouvelle (ajout puis retrait côté
  // serveur, une seule relecture à la fin : pas de passage par un état où elle serait aux deux endroits).
  const moveRule = async (file: string, from: string, rule: string, to: string, text: string) => {
    const old = ruleKey(file, from, rule)
    const neu = ruleKey(file, to, text)
    hide(old, true)
    setPending((p) => [...p, neu])
    hide(neu, false) // une règle retirée plus tôt puis remise doit s'afficher
    const undo = () => {
      hide(old, false)
      setPending((p) => p.filter((k) => k !== neu))
    }
    let a: CfgReply
    try {
      a = await api.configEdit({ file, op: "add", path: ["permissions", to], value: text })
    } catch (e) {
      return void (undo(), toast.error(errMsg(e)))
    }
    try {
      const r = await api.configEdit({ file, op: "remove", path: ["permissions", from], value: rule })
      toast.success(t("config.edit.ruleEdited"), { description: liveNotice(a.before, r.after, sessions), duration: 6000 })
    } catch (e) {
      hide(old, false) // l'ancienne règle existe toujours
      toast.error(errMsg(e))
    }
    await onDone()
    // L'ancienne place reste masquée : d'autres vues (onglet Projets) se relisent à part et la montreraient encore un instant.
    setPending((p) => p.filter((k) => k !== neu))
  }
  const propose = async (edit: CfgEdit) => {
    const rule = edit.op === "remove" && edit.path[0] === "permissions" && typeof edit.value === "string" ? ruleKey(edit.file, edit.path[1], edit.value) : ""
    if (rule) hide(rule, true)
    if (edit.op === "add" && edit.path[0] === "permissions" && typeof edit.value === "string") hide(ruleKey(edit.file, edit.path[1], edit.value), false)
    let r: CfgReply
    try {
      r = await api.configEdit(edit)
    } catch (e) {
      if (rule) hide(rule, false)
      return void toast.error(errMsg(e))
    }
    if (r.before === r.after) {
      if (rule) hide(rule, false)
      return void toast.info(t("config.shared.noChange"))
    }
    toast.success(r.out, { description: liveNotice(r.before, r.after, sessions), duration: 6000 })
    await onDone() // la règle reste masquée : les vues qui se relisent à part la montreraient encore un instant
  }
  // JSON brut : renvoie le message d'erreur (avec ligne et colonne) ou null si l'aperçu s'ouvre.
  const proposeRaw = async (file: string, text: string) => {
    let r: CfgReply
    try {
      r = await api.configRawEdit({ file, text, dry: true })
    } catch (e) {
      return errMsg(e)
    }
    if (r.before === r.after) {
      toast.info(t("config.shared.noChange"))
      return null
    }
    setPv({ raw: { file, text }, before: r.before, after: r.after, warnings: r.warnings ?? [] })
    return null
  }
  // Import de règles de permission : renvoie le message d'erreur, ou null si l'aperçu s'ouvre.
  // then : lancé après l'enregistrement réussi (abandonné si on annule), par exemple pour retirer la règle de son fichier d'origine.
  // now : l'aperçu a déjà été vu (éditeur texte) ; on écrit sans second dialogue.
  const proposePerms = async (file: string, text: string, mode: string, then?: () => void, now?: boolean) => {
    let r: CfgReply
    try {
      r = await api.configPermImport({ file, text, mode, dry: true })
    } catch (e) {
      return errMsg(e)
    }
    if (r.before === r.after) {
      toast.info(t("config.edit.alreadyThere"))
      return null
    }
    if (now) {
      let w: CfgReply
      try {
        w = await api.configPermImport({ file, text, mode, dry: false })
      } catch (e) {
        return errMsg(e)
      }
      toast.success(w.out, { description: liveNotice(w.before, w.after, sessions), duration: 9000 })
      onDone()
      then?.()
      return null
    }
    setPv({ then, perm: { file, text, mode }, summary: r.out, before: r.before, after: r.after, warnings: r.warnings ?? [] })
    return null
  }
  const apply = async () => {
    if (!pv) return
    setBusy(true)
    try {
      const r = pv.perm ? await api.configPermImport({ ...pv.perm, dry: false }) : pv.raw ? await api.configRawEdit({ ...pv.raw, dry: false }) : await api.configEdit(pv.edit!)
      toast.success(r.out, { description: liveNotice(r.before, r.after, sessions), duration: 9000 })
      setPv(null)
      onDone()
      pv.then?.()
    } catch (e) {
      toast.error(errMsg(e))
    } finally {
      setBusy(false)
    }
  }
  const dialog = (
    <Dialog open={!!pv} onOpenChange={(o) => !o && setPv(null)}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t("config.edit.dialogTitle")}</DialogTitle>
          <DialogDescription>
            {(pv?.edit?.file ?? pv?.raw?.file ?? pv?.perm?.file) === "u" ? "~/.claude/settings.json" : (pv?.edit?.file ?? pv?.raw?.file ?? pv?.perm?.file)}{pv?.summary ? ` — ${pv.summary}` : ""} {t("config.edit.backupNote")}
          </DialogDescription>
        </DialogHeader>
        {!!pv?.warnings.length && (
          <ul className="space-y-1 rounded-md border border-warning/40 bg-warning/10 p-2 text-xs">
            {pv.warnings.map((w, i) => (
              <li key={i}>⚠ {w.msg}</li>
            ))}
          </ul>
        )}
        <ScrollArea className="max-h-96 rounded-md border">{pv && <DiffView old={pv.before} cur={pv.after} compact />}</ScrollArea>
        <DialogFooter>
          <Button variant="ghost" onClick={() => setPv(null)}>
            {t("common.cancel")}
          </Button>
          <Button onClick={apply} disabled={busy}>
            {t("common.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
  return { gone, added, moveRule, propose, proposeRaw, proposePerms, dialog }
}
