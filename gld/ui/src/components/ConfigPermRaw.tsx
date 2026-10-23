import { useEffect, useMemo, useState } from "react"
import { CircleAlert, TriangleAlert } from "lucide-react"
import { formatRaw, parseRaw, type RawPerms } from "@/lib/permLint"
import { api, type CfgIssue } from "@/lib/api"
import { DiffView } from "./MemoryDiff"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"

type Doc = Record<string, unknown> | undefined

const rulesOf = (doc: Doc, k: string): string[] => {
  const v = (doc?.permissions as Record<string, unknown> | undefined)?.[k]
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : []
}

// Édition de toutes les règles (allow, ask, deny) et des dossiers supplémentaires d'un fichier en texte brut, vérifiée à la frappe. L'enregistrement passe par
// l'import en mode « remplacer » : aperçu des différences, sauvegarde, puis écriture.
function Editor({ file, label, doc, propose, onClose }: { file: string; label: string; doc: Doc; propose: (file: string, text: string, mode: string, then?: () => void, now?: boolean) => Promise<string | null>; onClose: () => void }) {
  const [text, setText] = useState(() => formatRaw({ allow: rulesOf(doc, "allow"), ask: rulesOf(doc, "ask"), deny: rulesOf(doc, "deny"), additionalDirectories: rulesOf(doc, "additionalDirectories") } as RawPerms))
  const [error, setError] = useState("")
  const { perms, lint } = useMemo(() => parseRaw(text), [text])
  const errors = lint.filter((l) => l.level === "error").length
  // Aperçu en direct : essai à blanc du serveur (différences et avertissements, dont les dossiers introuvables).
  const [last, setPv] = useState<{ before: string; after: string; warns: CfgIssue[] } | null>(null)
  const body = useMemo(() => JSON.stringify({ permissions: perms }), [perms])
  useEffect(() => {
    if (errors > 0) return
    let live = true
    const t = setTimeout(async () => {
      const r = (await api.post("/config-perm-import", { file, text: body, mode: "replace", dry: true })) as { ok: boolean; out: string; before: string; after: string; warnings?: CfgIssue[] }
      if (!live) return
      if (r.ok) setPv({ before: r.before, after: r.after, warns: r.warnings ?? [] })
      else setError(r.out)
    }, 400)
    return () => {
      live = false
      clearTimeout(t)
    }
  }, [body, errors, file])
  const pv = errors > 0 ? null : last
  const save = async () => {
    const err = await propose(file, body, "replace", undefined, true)
    if (err) setError(err)
    else onClose()
  }
  return (
    <DialogContent className="sm:max-w-3xl">
      <DialogHeader>
        <DialogTitle>Éditer les permissions en texte</DialogTitle>
        <DialogDescription>
          {label} : une règle par ligne sous [allow], [ask] ou [deny], un chemin par ligne sous [additionalDirectories], « # » pour un commentaire (non conservé). Le mode par défaut n'est pas touché.
        </DialogDescription>
      </DialogHeader>
      <textarea
        value={text}
        onChange={(e) => (setText(e.target.value), setError(""))}
        spellCheck={false}
        aria-label="Règles de permission"
        className="h-72 w-full resize-y rounded-md border bg-background p-3 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      />
      <ul className="max-h-32 space-y-0.5 overflow-y-auto text-xs" aria-label="Vérification">
        {lint.map((l, i) => (
          <li key={i} className={l.level === "error" ? "flex gap-1.5 text-destructive" : "flex gap-1.5 text-amber-600 dark:text-amber-400"}>
            {l.level === "error" ? <CircleAlert className="mt-0.5 size-3.5 shrink-0" /> : <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />}
            <span className="tabular-nums">Ligne {l.line}</span> {l.msg}
          </li>
        ))}
        {lint.length === 0 && <li className="text-muted-foreground">Rien à signaler : {perms.allow.length + perms.ask.length + perms.deny.length} règle(s), {perms.additionalDirectories.length} dossier(s).</li>}
      </ul>
      {pv && pv.before !== pv.after && (
        <>
          {pv.warns.length > 0 && (
            <ul className="space-y-0.5 text-xs text-amber-600 dark:text-amber-400" aria-label="Avertissements du serveur">
              {pv.warns.map((w, i) => (
                <li key={i}>⚠ {w.msg}</li>
              ))}
            </ul>
          )}
          <div className="max-h-48 overflow-auto rounded-md border" aria-label="Différences">
            <DiffView old={pv.before} cur={pv.after} compact />
          </div>
        </>
      )}
      {pv && pv.before === pv.after && errors === 0 && <p className="text-xs text-muted-foreground">Aucun changement par rapport au fichier.</p>}
      {error && <p className="text-sm text-destructive">{error}</p>}
      <DialogFooter>
        <Button variant="ghost" onClick={onClose}>
          Annuler
        </Button>
        <Button onClick={save} disabled={errors > 0 || !pv || pv.before === pv.after} title={errors ? "Corrige les erreurs pour continuer" : undefined}>
          Enregistrer
        </Button>
      </DialogFooter>
    </DialogContent>
  )
}

export function PermRawDialog({ open, ...p }: { open: boolean; file: string; label: string; doc: Doc; propose: (file: string, text: string, mode: string, then?: () => void, now?: boolean) => Promise<string | null>; onClose: () => void }) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && p.onClose()}>
      {open && <Editor key={p.file} {...p} />}
    </Dialog>
  )
}
