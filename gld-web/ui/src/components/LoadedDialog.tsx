import { useEffect, useState } from "react"
import { api, type LoadEvent } from "@/lib/api"
import { ago, tilde } from "@/lib/format"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"

const REASONS: Record<string, string> = {
  session_start: "au démarrage",
  nested_traversal: "dossier imbriqué",
  path_glob_match: "motif de chemin",
  include: "importé (@)",
  compact: "après compactage",
}

export const HOOK_SNIPPET = `"hooks": {
  "InstructionsLoaded": [
    { "hooks": [{ "type": "command", "command": "[ -x \\"$HOME/.local/bin/gld-web\\" ] && \\"$HOME/.local/bin/gld-web\\" _loaded; exit 0" }] }
  ]
}`

// Les fichiers d'instructions (CLAUDE.md, règles, imports) que Claude a réellement chargés dans une session, d'après le
// hook InstructionsLoaded. Sans hook installé, ou pour une session plus ancienne que lui, la liste est vide.
export function LoadedDialog({ id, home, open, onClose }: { id: string; home: string; open: boolean; onClose: () => void }) {
  const [files, setFiles] = useState<LoadEvent[] | null>(null)
  useEffect(() => {
    if (!open || !id) return
    let live = true
    api.sessionLoaded(id).then((r) => live && setFiles(r.files))
    return () => {
      live = false
    }
  }, [open, id])
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Instructions chargées</DialogTitle>
          <DialogDescription>Fichiers que Claude a lus dans cette session : CLAUDE.md, règles, imports.</DialogDescription>
        </DialogHeader>
        {files === null ? null : files.length === 0 ? (
          <div className="space-y-2 text-sm">
            <p>Rien d'enregistré pour cette session : le suivi n'est pas installé, ou la session est antérieure.</p>
            <p className="text-muted-foreground">
              Pour l'activer, ajoutez ceci aux réglages de Claude Code (<code>~/.claude/settings.json</code>) et lancez <code>make install</code> pour avoir <code>gld-web</code> dans <code>~/.local/bin</code> :
            </p>
            <pre className="overflow-x-auto rounded-lg border bg-muted/40 p-2 text-xs">{HOOK_SNIPPET}</pre>
          </div>
        ) : (
          <ScrollArea className="max-h-96">
            <ul className="space-y-1 pr-3 text-sm">
              {files.map((f, i) => (
                <li key={i} className="rounded-md px-2 py-1 hover:bg-muted">
                  <span className="break-all">{tilde(f.file, home)}</span>
                  <span className="block text-xs text-muted-foreground">
                    {REASONS[f.reason ?? ""] ?? f.reason}
                    {f.globs?.length ? ` · ${f.globs.join(", ")}` : ""}
                    {f.trigger ? ` · pour ${tilde(f.trigger, home)}` : ""} · {ago(f.at)}
                  </span>
                </li>
              ))}
            </ul>
          </ScrollArea>
        )}
      </DialogContent>
    </Dialog>
  )
}
