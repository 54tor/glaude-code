import { useEffect, useMemo, useState } from "react"
import { AlertTriangle, ArrowRight, BookMarked, FolderKanban, MessageSquareQuote, User, Wrench, type LucideIcon } from "lucide-react"
import { api, type Issue, type MemFile, type MemProject } from "@/lib/api"
import { tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Choose } from "./Choose"

// --- types d'entrée -------------------------------------------------------------------------------------

// Les quatre types de mémoire prévus par Claude Code : valeur du champ metadata.type, icône et aide.
export const TYPES: readonly { key: string; label: string; icon: LucideIcon; color: string; what: string; example: string }[] = [
  {
    key: "user",
    label: "utilisateur",
    icon: User,
    color: "text-sky-500",
    what: "Qui vous êtes : rôle, compétences, façon de travailler. Aide Claude à adapter ses explications.",
    example: "Développeur Go senior, découvre React ; préfère des réponses courtes.",
  },
  {
    key: "feedback",
    label: "retour",
    icon: MessageSquareQuote,
    color: "text-amber-500",
    what: "Une consigne ou une correction que vous avez donnée, avec la raison (Why) et la façon de l'appliquer (How to apply).",
    example: "Ne jamais ajouter de ligne d'attribution dans les commits.",
  },
  {
    key: "project",
    label: "projet",
    icon: FolderKanban,
    color: "text-emerald-500",
    what: "Un travail en cours, une décision ou une contrainte qui ne se déduit pas du code ni de git. Dates en absolu.",
    example: "Gel des fusions jusqu'au 5 novembre : release mobile en cours.",
  },
  {
    key: "reference",
    label: "référence",
    icon: BookMarked,
    color: "text-violet-500",
    what: "Où retrouver une information hors du dépôt : tableau de bord, ticket, document, canal.",
    example: "Les bugs de l'ingestion sont suivis dans le projet Linear « INGEST ».",
  },
]

export const typeOf = (t?: string) => TYPES.find((x) => x.key === t)

export function TypeHelp({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Les quatre types de mémoire</DialogTitle>
          <DialogDescription>
            Champ <code>metadata.type</code> du front matter d'une entrée. Il sert à ranger et retrouver les entrées ; il ne change pas ce que Claude charge.
          </DialogDescription>
        </DialogHeader>
        <ul className="space-y-3">
          {TYPES.map((t) => (
            <li key={t.key} className="flex gap-3">
              <t.icon className={cn("mt-0.5 size-5 shrink-0", t.color)} />
              <div className="min-w-0 text-sm">
                <p className="font-medium">
                  {t.label} <code className="text-xs font-normal text-muted-foreground">{t.key}</code>
                </p>
                <p>{t.what}</p>
                <p className="text-xs text-muted-foreground">Exemple : {t.example}</p>
              </div>
            </li>
          ))}
        </ul>
        <p className="text-xs text-muted-foreground">
          À ne pas mémoriser : ce que le code, git ou un CLAUDE.md disent déjà, et ce qui ne vaut que pour la conversation en cours.
        </p>
      </DialogContent>
    </Dialog>
  )
}

// --- différences entre deux versions --------------------------------------------------------------------

type Row = { op: " " | "-" | "+"; t: string }

// Différence ligne à ligne (plus longue sous-suite commune) ; les textes de mémoire sont courts.
export function lineDiff(a: string, b: string): Row[] {
  const x = a.split("\n")
  const y = b.split("\n")
  const n = x.length
  const m = y.length
  const l = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0))
  for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) l[i][j] = x[i] === y[j] ? l[i + 1][j + 1] + 1 : Math.max(l[i + 1][j], l[i][j + 1])
  const out: Row[] = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (x[i] === y[j]) {
      out.push({ op: " ", t: x[i++] })
      j++
    }
    else if (l[i + 1][j] >= l[i][j + 1]) out.push({ op: "-", t: x[i++] })
    else out.push({ op: "+", t: y[j++] })
  }
  while (i < n) out.push({ op: "-", t: x[i++] })
  while (j < m) out.push({ op: "+", t: y[j++] })
  return out
}

// old : version de la corbeille ; cur : version actuelle du fichier.
export function DiffView({ old, cur }: { old: string; cur: string }) {
  const rows = useMemo(() => lineDiff(old, cur), [old, cur])
  const same = rows.every((r) => r.op === " ")
  return (
    <div className="session-text mx-auto max-w-7xl px-6 py-6">
      <p className="mb-3 flex items-center gap-1.5 text-xs text-muted-foreground">
        Version de la corbeille <ArrowRight className="size-3" /> version actuelle {same && "· identiques"}
      </p>
      <pre className="whitespace-pre-wrap">
        {rows.map((r, i) => (
          <div
            key={i}
            className={cn(
              r.op === "+" && "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
              r.op === "-" && "bg-red-500/15 text-red-700 dark:text-red-300",
              r.op === " " && "text-muted-foreground"
            )}
          >
            {r.op} {r.t}
          </div>
        ))}
      </pre>
    </div>
  )
}

// --- portée d'une règle ---------------------------------------------------------------------------------

// Sépare des motifs par des virgules, hors accolades : « a/*.{ts,tsx}, b/** » donne deux motifs.
function splitGlobs(s: string): string[] {
  const out: string[] = []
  let depth = 0
  let cur = ""
  for (const c of s) {
    if (c === "{") depth++
    else if (c === "}") depth = Math.max(depth - 1, 0)
    if (c === "," && depth === 0) {
      out.push(cur)
      cur = ""
    } else cur += c
  }
  out.push(cur)
  return out.map((g) => g.trim()).filter(Boolean)
}

// Globs du champ `paths:` d'une règle globale, éditables, et aperçu des fichiers d'un projet qu'ils ciblent.
export function ScopeBar({ file, projects, home, onSaved }: { file: MemFile; projects: MemProject[]; home: string; onSaved: () => void }) {
  const [text, setText] = useState((file.paths ?? []).join(", "))
  const [busy, setBusy] = useState(false)
  const real = projects.filter((p) => !p.missing)
  const [dir, setDir] = useState(real[0]?.dir ?? "")
  const [res, setRes] = useState<{ files?: string[]; total?: number; truncated?: boolean; error?: string } | null>(null)
  const scoped = (file.paths?.length ?? 0) > 0
  const wanted = splitGlobs(text)
  const dirty = wanted.join("\n") !== (file.paths ?? []).join("\n")

  useEffect(() => {
    if (!scoped || !dir) return
    let live = true
    api.memoryMatches(file.key, dir).then((r) => live && setRes(r))
    return () => {
      live = false
    }
  }, [file.key, file.paths, scoped, dir])

  const save = async () => {
    setBusy(true)
    const r = await api.post("/memory-paths", { key: file.key, paths: wanted })
    setBusy(false)
    if (r.ok) onSaved()
    else setRes({ error: r.out })
  }
  return (
    <div className="space-y-2 border-b bg-muted/30 px-6 py-2 text-xs">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">Portée</span>
        <Input
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="toujours chargée — ou motifs séparés par des virgules : src/**/*.ts, **/*.php"
          className="h-7 min-w-64 flex-1 text-xs"
          aria-label="Motifs paths de la règle"
        />
        <Button size="xs" onClick={save} disabled={!dirty || busy}>
          Enregistrer
        </Button>
      </div>
      <p className="text-muted-foreground">
        {scoped
          ? "Chargée seulement quand Claude lit, écrit ou modifie un fichier qui correspond."
          : "Sans motif, la règle est chargée à chaque session. Les motifs sont relatifs à la racine du projet."}
      </p>
      {scoped && real.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-muted-foreground">Aperçu dans</span>
          <Choose
            value={dir}
            onChange={setDir}
            options={real.map((p) => [p.dir, tilde(p.project, home)] as const)}
            label="Projet de l'aperçu"
            className="min-w-0 max-w-72"
          />
          {res?.error ? (
            <span className="text-destructive">{res.error}</span>
          ) : res ? (
            <span className="text-muted-foreground">
              {res.total} fichier{(res.total ?? 0) > 1 ? "s" : ""} concerné{(res.total ?? 0) > 1 ? "s" : ""}
              {res.truncated && " (parcours interrompu)"}
            </span>
          ) : null}
        </div>
      )}
      {scoped && res?.files && res.files.length > 0 && (
        <p className="line-clamp-2 break-all text-muted-foreground">
          {res.files.slice(0, 12).join(" · ")}
          {(res.total ?? 0) > 12 && " …"}
        </p>
      )}
    </div>
  )
}

// --- santé -------------------------------------------------------------------------------------------

const KINDS: Record<string, string> = {
  "index-ligne": "Index : ligne orpheline",
  "index-absent": "Index : entrée absente",
  "index-long": "Index trop long",
  import: "Import cassé",
  lien: "Lien cassé",
  "front-matter": "Front matter",
  perime: "Entrée ancienne",
}

export function HealthDialog({
  open,
  issues,
  onClose,
  onOpenKey,
  onRepair,
}: {
  open: boolean
  issues: Issue[]
  onClose: () => void
  onOpenKey: (key: string) => void
  onRepair: (dir: string) => void
}) {
  const repairable = [...new Set(issues.filter((i) => i.dir && (i.kind === "index-ligne" || i.kind === "index-absent")).map((i) => i.dir!))]
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Santé de la mémoire</DialogTitle>
          <DialogDescription>
            {issues.length ? `${issues.length} point${issues.length > 1 ? "s" : ""} à vérifier. Rien n'est modifié sans votre clic.` : "Rien à signaler."}
          </DialogDescription>
        </DialogHeader>
        {repairable.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {repairable.map((d) => (
              <Button key={d} size="xs" variant="outline" onClick={() => onRepair(d)} title="Retire les lignes mortes et ajoute les entrées manquantes de MEMORY.md">
                <Wrench /> Réparer l'index de {d}
              </Button>
            ))}
          </div>
        )}
        <ScrollArea className="max-h-96">
          <ul className="space-y-1 pr-3">
            {issues.map((i, n) => (
              <li key={n}>
                <button onClick={() => (onOpenKey(i.key), onClose())} className="flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted">
                  <AlertTriangle className={cn("mt-0.5 size-3.5 shrink-0", i.kind === "perime" ? "text-muted-foreground" : "text-amber-500")} />
                  <span className="min-w-0">
                    <span className="block text-xs font-medium text-muted-foreground">
                      {KINDS[i.kind] ?? i.kind} · {i.key}
                    </span>
                    <span className="break-words">{i.msg}</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
