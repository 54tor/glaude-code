import { useEffect, useState } from "react"
import { api, type MemFile, type MemProject } from "@/lib/api"
import { tilde } from "@/lib/format"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Choose } from "./Choose"

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
