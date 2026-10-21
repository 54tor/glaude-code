import { useEffect, useState } from "react"
import { api, type Detail, type Session } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { useActions } from "./actions"

const fmt = (x?: string | number) => (x ? new Date(x).toLocaleString("fr-FR") : "?")

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex gap-2">
      <span className="w-20 shrink-0 text-muted-foreground">{k}</span>
      <span className="break-all">{v}</span>
    </div>
  )
}

// Panneau de détails, chargé à l'ouverture seulement.
function Details({ s }: { s: Session }) {
  const [d, setD] = useState<Detail | null>(null)
  useEffect(() => {
    api.info(s).then(setD)
  }, [s])
  if (!d) return <p className="text-muted-foreground">Chargement…</p>
  return (
    <div className="space-y-1">
      <Row k="id" v={s.id} />
      <Row k="début" v={fmt(d.t0)} />
      <Row k="fin" v={fmt(d.t1)} />
      <Row k="activité" v={`${d.n} prompt(s) · ${d.subagents} sous-agent(s)`} />
      <Row k="branche" v={d.branch || "—"} />
      <Row k="modèles" v={d.models.join(", ") || "—"} />
      <p className="mt-2 font-medium">Derniers prompts</p>
      {d.prompts.map((p, i) => (
        <p key={i} className="mt-1 border-l-2 pl-2 whitespace-pre-wrap text-muted-foreground">
          {p}
        </p>
      ))}
    </div>
  )
}

export function SessionRow({ s }: { s: Session }) {
  const a = useActions()
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const copy = () => {
    navigator.clipboard.writeText(`claude --resume ${s.id}`)
    setCopied(true)
    setTimeout(() => setCopied(false), 1200)
  }
  return (
    <Collapsible open={open} onOpenChange={setOpen} className="border-b last:border-0">
      <div className="flex items-center gap-2 px-3 py-1.5 hover:bg-muted/50">
        <CollapsibleTrigger asChild>
          <button
            title={s.title}
            className={`min-w-0 flex-1 truncate text-left ${s.named ? "font-medium" : "text-muted-foreground italic"}`}
          >
            {s.title}
          </button>
        </CollapsibleTrigger>
        <span className="hidden text-xs whitespace-nowrap text-muted-foreground sm:block">
          {new Date(s.mtime * 1000).toLocaleString("fr-FR", { dateStyle: "short", timeStyle: "short" })}
        </span>
        <Badge variant="outline">{s.kb} Ko</Badge>
        <Button variant="ghost" size="xs" className="font-mono text-muted-foreground" onClick={copy} title="copier « claude --resume »">
          {copied ? "copié ✓" : s.id.slice(0, 8)}
        </Button>
        <Button variant="outline" size="xs" onClick={() => a.read(s)} title="afficher toute la conversation">
          Lire
        </Button>
        <Button variant="outline" size="xs" onClick={() => a.open({ dir: s.dir, id: s.id })} title="claude --resume dans un nouvel onglet de terminal">
          Ouvrir
        </Button>
        <Button variant="outline" size="xs" onClick={() => a.move(s)} title="déplacer cette session vers un autre dossier">
          Déplacer…
        </Button>
        <Button variant="destructive" size="xs" onClick={() => a.remove([{ dir: s.dir, id: s.id }], `« ${s.title} »`)}>
          Supprimer
        </Button>
      </div>
      <CollapsibleContent>
        <div className="border-t bg-muted/40 px-4 py-3 text-xs">{open && <Details s={s} />}</div>
      </CollapsibleContent>
    </Collapsible>
  )
}
