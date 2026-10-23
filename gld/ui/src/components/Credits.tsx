import { useState } from "react"
import { Coffee, Copy, Heart } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"

// Adresses de soutien : vides, la section correspondante n'est pas affichée.
const XMR = ""
const BTC = ""

const GO = [
  ["modernc.org/sqlite", "https://gitlab.com/cznic/sqlite"],
  ["coder/websocket", "https://github.com/coder/websocket"],
  ["creack/pty", "https://github.com/creack/pty"],
  ["bmatcuk/doublestar", "https://github.com/bmatcuk/doublestar"],
  ["yaml.v3", "https://github.com/go-yaml/yaml"],
]
const WEB = [
  ["React", "https://react.dev"],
  ["Vite", "https://vite.dev"],
  ["Tailwind CSS", "https://tailwindcss.com"],
  ["Radix UI / shadcn", "https://ui.shadcn.com"],
  ["lucide", "https://lucide.dev"],
  ["wouter", "https://github.com/molefrog/wouter"],
  ["react-markdown", "https://github.com/remarkjs/react-markdown"],
  ["xterm.js", "https://xtermjs.org"],
  ["sonner", "https://sonner.emilkowal.ski"],
  ["Geist, Inter, IBM Plex, JetBrains Mono, Fira Code", "https://fontsource.org"],
]

function Libs({ title, rows }: { title: string; rows: string[][] }) {
  return (
    <div>
      <h3 className="mb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">{title}</h3>
      <p className="text-sm leading-6">
        {rows.map(([n, u], i) => (
          <span key={n}>
            {i > 0 && ", "}
            <a href={u} target="_blank" rel="noreferrer" className="underline-offset-2 hover:underline">
              {n}
            </a>
          </span>
        ))}
      </p>
    </div>
  )
}

function Addr({ label, value }: { label: string; value: string }) {
  if (!value) return null
  const copy = () => navigator.clipboard.writeText(value).then(() => toast.success(`Adresse ${label} copiée`), () => toast.error("Copie impossible"))
  return (
    <div className="flex items-center gap-2 rounded-lg border bg-muted/40 px-3 py-2">
      <span className="w-10 shrink-0 text-xs font-semibold">{label}</span>
      <code className="min-w-0 flex-1 truncate font-mono text-xs" title={value}>
        {value}
      </code>
      <Button variant="ghost" size="icon-sm" onClick={copy} aria-label={`Copier l'adresse ${label}`}>
        <Copy />
      </Button>
    </div>
  )
}

export function CreditsButton() {
  const [open, setOpen] = useState(false)
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="Crédits et soutien" title="Crédits et soutien">
          <Heart />
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>gld</DialogTitle>
          <DialogDescription>
            Écrit par Sat0r, publié sous licence MIT (2026).{" "}
            <a href="https://github.com/54tor/glaude-code" target="_blank" rel="noreferrer" className="underline-offset-2 hover:underline">
              Code source
            </a>
            .
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <Libs title="Côté serveur (Go)" rows={GO} />
          <Libs title="Côté navigateur" rows={WEB} />
          {(XMR || BTC) && (
            <div className="space-y-2 border-t pt-4">
              <h3 className="flex items-center gap-2 text-sm font-medium">
                <Coffee className="size-4" /> Offrir un café
              </h3>
              <Addr label="XMR" value={XMR} />
              <Addr label="BTC" value={BTC} />
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
