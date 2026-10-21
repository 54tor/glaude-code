import { useEffect, useRef, useState } from "react"
import { Brain, FileText, Globe, Send } from "lucide-react"
import { toast } from "sonner"
import { api, type MemFile, type MemProject, type Snippet } from "@/lib/api"
import { tilde } from "@/lib/format"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useActions } from "./actions"
import type { Choice } from "@/lib/models"
import { ModelPicker } from "./ModelPicker"
import { Choose } from "./Choose"

// Dossier où lancer une nouvelle session : celui d'une de ses sessions (le serveur en tire le dossier de travail).
export type Target = { label: string; project: string; dir: string; id: string }

// Ce que la fenêtre de saisie envoie : un message de reprise, ou le prompt d'une nouvelle session.
export type ComposeAsk = {
  id: string // clé de remise à zéro du formulaire
  title: string
  subtitle: string
  project: string // pour proposer d'abord la mémoire de ce projet
  initial?: string
  send: string // libellé du bouton
  choice: Choice // modèle et effort proposés par défaut (Réglages)
  targets?: Target[] // plusieurs : un menu pour choisir le dossier (le premier par défaut)
  run: (text: string, choice: Choice, target?: Target) => void
}

// Saisie d'un message, éventuellement composé à partir de snippets et d'entrées de mémoire.
export function Compose({ ask, snippets, onClose }: { ask: ComposeAsk | null; snippets: Snippet[]; onClose: () => void }) {
  return (
    <Dialog open={!!ask} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">{ask && <Form key={ask.id} ask={ask} snippets={snippets} onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

function Form({ ask, snippets, onClose }: { ask: ComposeAsk; snippets: Snippet[]; onClose: () => void }) {
  const a = useActions()
  const [text, setText] = useState(ask.initial ?? "")
  const [choice, setChoice] = useState(ask.choice)
  const [target, setTarget] = useState(0)
  const tgt = ask.targets?.[target]
  const project = tgt?.project ?? ask.project
  const [mem, setMem] = useState<{ global: MemFile[]; projects: MemProject[] } | null>(null)
  const area = useRef<HTMLTextAreaElement>(null)
  const ok = text.trim() !== ""
  useEffect(() => {
    let live = true
    api.memory().then((m) => live && setMem(m))
    return () => {
      live = false
    }
  }, [])
  // La mémoire du projet d'abord, puis la globale, puis les autres projets.
  const own = mem?.projects.filter((p) => p.project === project) ?? []
  const others = mem?.projects.filter((p) => p.project !== project) ?? []

  const send = () => {
    if (!ok) return
    onClose()
    ask.run(text.trim(), choice, tgt)
  }
  // Insère à la position du curseur (ou à la place de la sélection), puis y replace le curseur.
  const put = (t: string) => {
    const el = area.current
    const i = el?.selectionStart ?? text.length
    const j = el?.selectionEnd ?? text.length
    setText(text.slice(0, i) + t + text.slice(j))
    requestAnimationFrame(() => {
      el?.focus()
      el?.setSelectionRange(i + t.length, i + t.length)
    })
  }
  const insert = async (path: string) => {
    const r = await api.snippet(path)
    if (r.text === undefined) return void toast.error(r.error ?? "Snippet illisible")
    put(r.text)
  }
  // Une entrée de mémoire, citée telle quelle ; il reste à écrire la demande à la suite.
  const insertMemory = async (f: MemFile, scope: string) => {
    const r = await api.memoryFile(f.key)
    if (r.text === undefined) return void toast.error(r.error ?? "Mémoire illisible")
    const fence = r.text.includes("```") ? "~~~~" : "```"
    const where = f.key.startsWith("m/") ? `mémoire de ${scope} : ${f.name}` : `${scope}/${f.name}`
    put(`D'après cette entrée memory (${where}) :\n\n${fence}markdown\n${r.text.trim()}\n${fence}\n\n`)
  }
  const memItems = (files: MemFile[], scope: string) =>
    files.map((f) => (
      <DropdownMenuItem key={f.key} onSelect={() => insertMemory(f, scope)}>
        <FileText /> <span className="truncate">{f.name}</span>
      </DropdownMenuItem>
    ))

  return (
    <div className="grid min-w-0 gap-4">
      <DialogHeader>
        <DialogTitle>{ask.title}</DialogTitle>
        <DialogDescription className="truncate">{ask.subtitle}</DialogDescription>
      </DialogHeader>
      {ask.targets && ask.targets.length > 1 && (
        <div className="flex items-center gap-2 text-sm">
          <span className="shrink-0 text-muted-foreground">Dossier</span>
          <Choose
            value={String(target)}
            onChange={(v) => setTarget(Number(v))}
            options={ask.targets.map((t, i) => [String(i), t.label] as const)}
            label="Dossier de la nouvelle session"
            size="default"
            className="min-w-0 flex-1"
          />
        </div>
      )}
      <textarea
        ref={area}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "Enter" && (e.preventDefault(), send())}
        placeholder="Message envoyé à Claude…"
        autoFocus
        onFocus={(e) => e.target.setSelectionRange(e.target.value.length, e.target.value.length)}
        rows={8}
        maxLength={32000}
        className="session-text w-full resize-y rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        aria-label="Message"
      />
      <DialogFooter className="flex-wrap items-center gap-2 sm:justify-between">
        <div className="flex flex-wrap gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" disabled={!mem}>
                <Brain /> Insérer une mémoire
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent onCloseAutoFocus={(e) => e.preventDefault()} align="start" className="max-h-80 w-80 overflow-y-auto">
              {own.map((p) => (
                <div key={p.dir}>
                  <DropdownMenuLabel className="flex items-center gap-1.5">
                    <Brain className="size-3.5" /> Ce projet
                  </DropdownMenuLabel>
                  {memItems(p.files, tilde(p.project, a.home))}
                  <DropdownMenuSeparator />
                </div>
              ))}
              {!!mem?.global.length && (
                <>
                  <DropdownMenuLabel className="flex items-center gap-1.5">
                    <Globe className="size-3.5" /> Globale
                  </DropdownMenuLabel>
                  {memItems(mem.global, "~/.claude")}
                </>
              )}
              {others.map((p) => (
                <div key={p.dir}>
                  <DropdownMenuSeparator />
                  <DropdownMenuLabel className="truncate">{tilde(p.project, a.home)}</DropdownMenuLabel>
                  {memItems(p.files, tilde(p.project, a.home))}
                </div>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          {snippets.length > 0 && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm">
                  <FileText /> Insérer un snippet
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent onCloseAutoFocus={(e) => e.preventDefault()} align="start" className="max-h-72 w-72 overflow-y-auto">
                {snippets.map((s) => (
                  <DropdownMenuItem key={s.path} onSelect={() => insert(s.path)}>
                    <FileText /> <span className="truncate">{s.path}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          <ModelPicker value={choice} onChange={setChoice} className="max-w-80" />
          <Button variant="outline" onClick={onClose}>
            Annuler
          </Button>
          <Button onClick={send} disabled={!ok} title="Ctrl+Entrée">
            <Send /> {ask.send}
          </Button>
        </div>
      </DialogFooter>
    </div>
  )
}
