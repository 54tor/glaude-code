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
    .filter((diffLine) => diffLine.op !== " ")
    .map((diffLine) => diffLine.t)
    .join("\n")
  const manualSteps: string[] = []
  if (/"model"\s*:/.test(changed)) manualSteps.push(i18n.t("config.edit.model"))
  if (/"(effortLevel|modelSettings)"\s*:/.test(changed)) manualSteps.push(i18n.t("config.edit.effort"))
  if (/"enabledPlugins"|"pluginConfigs"|"[\w.-]+@[\w.-]+"\s*:/.test(changed)) manualSteps.push(i18n.t("config.edit.plugins"))
  const live = sessions ? i18n.t("config.edit.live", { count: sessions }) : i18n.t("config.edit.liveNone")
  if (!manualSteps.length) return live
  return i18n.t("config.edit.manual", { live, todo: manualSteps.join(i18n.t("config.edit.sep")) })
}

const SIMPLE_EDIT_TOAST_DURATION_MS = 6000
const CONFIRMED_EDIT_TOAST_DURATION_MS = 9000

type Preview = { then?: () => void; edit?: CfgEdit; raw?: { file: string; text: string }; perm?: { file: string; text: string; mode: string }; summary?: string; before: string; after: string; warnings: CfgIssue[] }

// Toute modification d'un fichier de réglages passe par ici : aperçu des différences, confirmation, puis écriture
// (le serveur fait une sauvegarde avant). Renvoie propose() et le dialogue à afficher.
export function useConfigEdit(onDone: () => void | Promise<unknown>, sessions = 0): { gone: (file: string, kind: string, rule: string) => boolean; added: (file: string, kind: string) => string[]; moveRule: (file: string, from: string, rule: string, to: string, text: string) => Promise<void>; propose: (e: CfgEdit) => Promise<void>; proposeRaw: (file: string, text: string) => Promise<string | null>; proposePerms: (file: string, text: string, mode: string, then?: () => void, now?: boolean) => Promise<string | null>; dialog: ReactNode } {
  const { t } = useTranslation()
  const [preview, setPreview] = useState<Preview | null>(null)
  const [busy, setBusy] = useState(false)
  // Modification simple (une règle, un réglage, une variable) : appliquée tout de suite, sans fenêtre de confirmation. Le serveur
  // fait une sauvegarde avant d'écrire (onglet Sauvegardes) ; le toast dit ce que les sessions ouvertes en voient.
  // Retrait d'une règle : la ligne disparaît tout de suite (gone) et ne revient qu'en cas d'échec.
  const [hidden, setHidden] = useState<Set<string>>(new Set())
  const hide = (hiddenKey: string, shouldHide: boolean) =>
    setHidden((previouslyHidden) => {
      const nextHidden = new Set(previouslyHidden)
      if (shouldHide) nextHidden.add(hiddenKey)
      else nextHidden.delete(hiddenKey)
      return nextHidden
    })
  const ruleKey = (file: string, kind: string, rule: string) => JSON.stringify([file, kind, rule])
  const gone = (file: string, kind: string, rule: string) => hidden.has(ruleKey(file, kind, rule))
  // Règles ajoutées en attente d'écriture : affichées tout de suite.
  const [pending, setPending] = useState<string[]>([])
  const added = (file: string, kind: string) =>
    pending
      .map((pendingKey) => JSON.parse(pendingKey) as string[])
      .filter(([pendingFile, pendingKind]) => pendingFile === file && pendingKind === kind)
      .map(([, , pendingRule]) => pendingRule)
  // Changer une règle de catégorie ou de texte : elle passe d'un coup de l'ancienne place à la nouvelle (ajout puis retrait côté
  // serveur, une seule relecture à la fin : pas de passage par un état où elle serait aux deux endroits).
  const moveRule = async (file: string, from: string, rule: string, to: string, text: string) => {
    const oldRuleKey = ruleKey(file, from, rule)
    const newRuleKey = ruleKey(file, to, text)
    const withoutNewRule = (pendingKeys: string[]) => pendingKeys.filter((pendingKey) => pendingKey !== newRuleKey)
    hide(oldRuleKey, true)
    setPending((pendingKeys) => [...pendingKeys, newRuleKey])
    hide(newRuleKey, false) // une règle retirée plus tôt puis remise doit s'afficher
    const undo = () => {
      hide(oldRuleKey, false)
      setPending(withoutNewRule)
    }
    let addReply: CfgReply
    try {
      addReply = await api.configEdit({ file, op: "add", path: ["permissions", to], value: text })
    } catch (error) {
      undo()
      toast.error(errMsg(error))
      return
    }
    try {
      const removeReply = await api.configEdit({ file, op: "remove", path: ["permissions", from], value: rule })
      toast.success(t("config.edit.ruleEdited"), { description: liveNotice(addReply.before, removeReply.after, sessions), duration: SIMPLE_EDIT_TOAST_DURATION_MS })
    } catch (error) {
      hide(oldRuleKey, false) // l'ancienne règle existe toujours
      toast.error(errMsg(error))
    }
    await onDone()
    // L'ancienne place reste masquée : d'autres vues (onglet Projets) se relisent à part et la montreraient encore un instant.
    setPending(withoutNewRule)
  }
  const propose = async (edit: CfgEdit) => {
    const isPermissionRule = edit.path[0] === "permissions" && typeof edit.value === "string"
    const removedRuleKey = edit.op === "remove" && isPermissionRule ? ruleKey(edit.file, edit.path[1], edit.value as string) : ""
    if (removedRuleKey) hide(removedRuleKey, true)
    if (edit.op === "add" && isPermissionRule) hide(ruleKey(edit.file, edit.path[1], edit.value as string), false)
    let reply: CfgReply
    try {
      reply = await api.configEdit(edit)
    } catch (error) {
      if (removedRuleKey) hide(removedRuleKey, false)
      return void toast.error(errMsg(error))
    }
    if (reply.before === reply.after) {
      if (removedRuleKey) hide(removedRuleKey, false)
      return void toast.info(t("config.shared.noChange"))
    }
    toast.success(reply.out, { description: liveNotice(reply.before, reply.after, sessions), duration: SIMPLE_EDIT_TOAST_DURATION_MS })
    await onDone() // la règle reste masquée : les vues qui se relisent à part la montreraient encore un instant
  }
  // JSON brut : renvoie le message d'erreur (avec ligne et colonne) ou null si l'aperçu s'ouvre.
  const proposeRaw = async (file: string, text: string) => {
    let dryRunReply: CfgReply
    try {
      dryRunReply = await api.configRawEdit({ file, text, dry: true })
    } catch (error) {
      return errMsg(error)
    }
    if (dryRunReply.before === dryRunReply.after) {
      toast.info(t("config.shared.noChange"))
      return null
    }
    setPreview({ raw: { file, text }, before: dryRunReply.before, after: dryRunReply.after, warnings: dryRunReply.warnings ?? [] })
    return null
  }
  // Import de règles de permission : renvoie le message d'erreur, ou null si l'aperçu s'ouvre.
  // then : lancé après l'enregistrement réussi (abandonné si on annule), par exemple pour retirer la règle de son fichier d'origine.
  // now : l'aperçu a déjà été vu (éditeur texte) ; on écrit sans second dialogue.
  const proposePerms = async (file: string, text: string, mode: string, then?: () => void, now?: boolean) => {
    let dryRunReply: CfgReply
    try {
      dryRunReply = await api.configPermImport({ file, text, mode, dry: true })
    } catch (error) {
      return errMsg(error)
    }
    if (dryRunReply.before === dryRunReply.after) {
      toast.info(t("config.edit.alreadyThere"))
      return null
    }
    if (now) {
      let writeReply: CfgReply
      try {
        writeReply = await api.configPermImport({ file, text, mode, dry: false })
      } catch (error) {
        return errMsg(error)
      }
      toast.success(writeReply.out, { description: liveNotice(writeReply.before, writeReply.after, sessions), duration: CONFIRMED_EDIT_TOAST_DURATION_MS })
      onDone()
      then?.()
      return null
    }
    setPreview({ then, perm: { file, text, mode }, summary: dryRunReply.out, before: dryRunReply.before, after: dryRunReply.after, warnings: dryRunReply.warnings ?? [] })
    return null
  }
  const writePreview = (previewToWrite: Preview) => {
    if (previewToWrite.perm) return api.configPermImport({ ...previewToWrite.perm, dry: false })
    if (previewToWrite.raw) return api.configRawEdit({ ...previewToWrite.raw, dry: false })
    return api.configEdit(previewToWrite.edit!)
  }
  const apply = async () => {
    if (!preview) return
    setBusy(true)
    try {
      const reply = await writePreview(preview)
      toast.success(reply.out, { description: liveNotice(reply.before, reply.after, sessions), duration: CONFIRMED_EDIT_TOAST_DURATION_MS })
      setPreview(null)
      onDone()
      preview.then?.()
    } catch (error) {
      toast.error(errMsg(error))
    } finally {
      setBusy(false)
    }
  }
  const previewFile = preview?.edit?.file ?? preview?.raw?.file ?? preview?.perm?.file
  const previewFileLabel = previewFile === "u" ? "~/.claude/settings.json" : previewFile
  const dialog = (
    <Dialog open={!!preview} onOpenChange={(isOpen) => !isOpen && setPreview(null)}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t("config.edit.dialogTitle")}</DialogTitle>
          <DialogDescription>
            {previewFileLabel}{preview?.summary ? ` — ${preview.summary}` : ""} {t("config.edit.backupNote")}
          </DialogDescription>
        </DialogHeader>
        {!!preview?.warnings.length && (
          <ul className="space-y-1 rounded-md border border-warning/40 bg-warning/10 p-2 text-xs">
            {preview.warnings.map((warning, warningIndex) => (
              <li key={warningIndex}>⚠ {warning.msg}</li>
            ))}
          </ul>
        )}
        <ScrollArea className="max-h-96 rounded-md border">{preview && <DiffView old={preview.before} cur={preview.after} compact />}</ScrollArea>
        <DialogFooter>
          <Button variant="ghost" onClick={() => setPreview(null)}>
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
