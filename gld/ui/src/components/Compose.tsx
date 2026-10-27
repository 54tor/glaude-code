import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Brain, FileText, FolderSearch, Globe, Send } from "lucide-react"
import { toast } from "sonner"
import { api, type MemFile, type MemProject } from "@/lib/api"
import { tilde } from "@/lib/format"
import { argAt } from "@/lib/commands"
import { isSend, keyHint } from "@/lib/utils"
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
import { useMention } from "./Mention"
import { Choose } from "./Choose"
import { SnippetMenu } from "./SnippetMenu"

// Dossier où lancer une nouvelle session : celui d'une de ses sessions (le serveur en tire le dossier de travail).
export type Target = { label: string; project: string; dir: string; id: string; cwd?: string } // cwd : dossier choisi à la main (sans session existante)

// Ce que la fenêtre de saisie envoie : un message de reprise, ou le prompt d'une nouvelle session.
export type ComposeAsk = {
  id: string // clé de remise à zéro du formulaire
  title: string
  subtitle: string
  project: string // pour proposer d'abord la mémoire de ce projet
  initial?: string
  send: string // libellé du bouton
  warning?: string // avertissement affiché au-dessus de la saisie (ex. coût en tokens)
  choice: Choice // modèle et effort proposés par défaut (Réglages)
  outputs?: { key: string; label: string }[] // fusion : le document de sortie ; en changer régénère le texte par reprompt
  reprompt?: (output: string) => Promise<string | undefined>
  targets?: Target[] // plusieurs : un menu pour choisir le dossier (le premier par défaut)
  run: (text: string, choice: Choice, target?: Target, output?: string) => Promise<boolean> // false : la fenêtre reste ouverte, le texte n'est pas perdu
}

// Saisie d'un message, éventuellement composé à partir de snippets et d'entrées de mémoire.
export function Compose({ ask, onClose }: { ask: ComposeAsk | null; onClose: () => void }) {
  return (
    <Dialog open={!!ask} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">{ask && <Form key={ask.id} ask={ask} onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

function Form({ ask, onClose }: { ask: ComposeAsk; onClose: () => void }) {
  const { t: tr } = useTranslation()
  const a = useActions()
  const [text, setText] = useState(ask.initial ?? "")
  const mention = useMention(text, setText)
  const [choice, setChoice] = useState(ask.choice)
  const [target, setTarget] = useState(0)
  const [output, setOutput] = useState(ask.outputs?.[0]?.key)
  const [custom, setCustom] = useState<Target | null>(null) // dossier choisi dans l'explorateur
  const tgt = custom ?? ask.targets?.[target]
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

  const [busy, setBusy] = useState(false)
  // Fermée seulement si l'envoi réussit : en cas d'échec, le texte reste là pour réessayer ou le copier.
  const send = async () => {
    if (!ok || busy) return
    setBusy(true)
    const done = await ask.run(text.trim(), choice, tgt, output)
    setBusy(false)
    if (done) onClose()
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
  // Une commande : son corps, sans en-tête ; son premier $ARGUMENTS est sélectionné pour être remplacé en tapant.
  const insert = (t: string) => {
    const el = area.current
    const i = el?.selectionStart ?? text.length
    put(t)
    const arg = argAt(t)
    if (arg) requestAnimationFrame(() => requestAnimationFrame(() => el?.setSelectionRange(i + arg.at, i + arg.at + arg.len)))
  }
  // Une entrée de mémoire, citée telle quelle ; il reste à écrire la demande à la suite.
  const insertMemory = async (f: MemFile, scope: string) => {
    const r = await api.memoryFile(f.key)
    if (r.text === undefined) return void toast.error(r.error ?? tr("conversation.compose.memoryUnreadable"))
    const fence = r.text.includes("```") ? "~~~~" : "```"
    const where = f.key.startsWith("m/") ? tr("conversation.compose.memoryScope", { scope, name: f.name }) : `${scope}/${f.name}`
    put(`${tr("conversation.compose.memoryIntro", { where })}\n\n${fence}markdown\n${r.text.trim()}\n${fence}\n\n`)
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
      {ask.warning && (
        <p role="alert" className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-sm">
          {ask.warning}
        </p>
      )}
      {ask.outputs && (
        <div className="flex items-center gap-2 text-sm">
          <span className="shrink-0 text-muted-foreground">{tr("conversation.compose.output")}</span>
          <Choose
            value={output ?? ""}
            onChange={async (v) => {
              const t = await ask.reprompt?.(v)
              if (t === undefined) return
              setOutput(v)
              setText(t)
            }}
            options={ask.outputs.map((o) => [o.key, o.label] as const)}
            label={tr("conversation.compose.outputAria")}
            size="default"
            className="min-w-0 flex-1"
          />
        </div>
      )}
      {ask.targets && (
        <div className="flex items-center gap-2 text-sm">
          <span className="shrink-0 text-muted-foreground">{tr("conversation.compose.folder")}</span>
          <Choose
            value={custom ? "custom" : String(target)}
            onChange={(v) => (v === "custom" ? null : (setCustom(null), setTarget(Number(v))))}
            options={[...ask.targets.map((t, i) => [String(i), t.label] as const), ...(custom ? [["custom", custom.label] as const] : [])]}
            label={tr("conversation.compose.newSessionFolder")}
            size="default"
            className="min-w-0 flex-1"
          />
          <Button
            type="button"
            variant="outline"
            onClick={async () => {
              const p = await a.pickDir(tr("conversation.compose.newSessionFolder"),tgt?.project ?? a.home)
              if (p) setCustom({ label: tilde(p, a.home), project: p, dir: "", id: "", cwd: p })
            }}
          >
            <FolderSearch /> {tr("conversation.compose.otherFolder")}
          </Button>
        </div>
      )}
      {mention.list}
      <textarea
        ref={area}
        value={text}
        onChange={(e) => (setText(e.target.value), mention.track(e.target))}
        onKeyDown={(e) => !mention.onKey(e) && isSend(e) && (e.preventDefault(), send())}
        placeholder={keyHint(tr("conversation.compose.placeholder"))}
        autoFocus
        onFocus={(e) => {
          // Texte prérempli d'une commande : son $ARGUMENTS sélectionné ; sinon le curseur à la fin.
          const arg = argAt(e.target.value)
          if (arg) e.target.setSelectionRange(arg.at, arg.at + arg.len)
          else e.target.setSelectionRange(e.target.value.length, e.target.value.length)
        }}
        rows={8}
        maxLength={32000}
        className="session-text w-full resize-y rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        aria-label={tr("conversation.compose.messageAria")}
      />
      <DialogFooter className="flex-wrap items-center gap-2 sm:justify-between">
        <div className="flex flex-wrap gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" disabled={!mem}>
                <Brain /> {tr("conversation.compose.insertMemory")}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent onCloseAutoFocus={(e) => e.preventDefault()} align="start" className="max-h-80 w-80 overflow-y-auto">
              {own.map((p) => (
                <div key={p.dir}>
                  <DropdownMenuLabel className="flex items-center gap-1.5">
                    <Brain className="size-3.5" /> {tr("conversation.compose.thisProject")}
                  </DropdownMenuLabel>
                  {memItems(p.files, tilde(p.project, a.home))}
                  <DropdownMenuSeparator />
                </div>
              ))}
              {!!mem?.global.length && (
                <>
                  <DropdownMenuLabel className="flex items-center gap-1.5">
                    <Globe className="size-3.5" /> {tr("conversation.compose.global")}
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
          <SnippetMenu onInsert={insert} />
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          <ModelPicker value={choice} onChange={setChoice} className="max-w-80" />
          <Button variant="outline" onClick={onClose}>
            {tr("common.cancel")}
          </Button>
          <Button onClick={send} disabled={!ok || busy} title={tr("conversation.compose.sendTitle")}>
            <Send /> {ask.send}
          </Button>
        </div>
      </DialogFooter>
    </div>
  )
}
