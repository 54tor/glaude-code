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
import { AttachButton, ImageChips, useImages } from "@/hooks/useImages"

const MAX_MESSAGE_LENGTH = 32000
const CUSTOM_TARGET_VALUE = "custom"

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
    <Dialog open={!!ask} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="sm:max-w-2xl">{ask && <Form key={ask.id} ask={ask} onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

function Form({ ask, onClose }: { ask: ComposeAsk; onClose: () => void }) {
  const { t } = useTranslation()
  const actions = useActions()
  const [text, setText] = useState(ask.initial ?? "")
  const mention = useMention(text, setText)
  const [choice, setChoice] = useState(ask.choice)
  const [target, setTarget] = useState(0)
  const [output, setOutput] = useState(ask.outputs?.[0]?.key)
  const [custom, setCustom] = useState<Target | null>(null) // dossier choisi dans l'explorateur
  const selectedTarget = custom ?? ask.targets?.[target]
  const project = selectedTarget?.project ?? ask.project
  const [memory, setMemory] = useState<{ global: MemFile[]; projects: MemProject[] } | null>(null)
  const textArea = useRef<HTMLTextAreaElement>(null)
  const pictures = useImages()
  const canSend = text.trim() !== "" || pictures.images.length > 0
  useEffect(() => {
    let isMounted = true
    api.memory().then((loadedMemory) => isMounted && setMemory(loadedMemory))
    return () => {
      isMounted = false
    }
  }, [])
  // La mémoire du projet d'abord, puis la globale, puis les autres projets.
  const ownProjects = memory?.projects.filter((memoryProject) => memoryProject.project === project) ?? []
  const otherProjects = memory?.projects.filter((memoryProject) => memoryProject.project !== project) ?? []

  const [busy, setBusy] = useState(false)
  // Fermée seulement si l'envoi réussit : en cas d'échec, le texte reste là pour réessayer ou le copier.
  const send = async () => {
    if (!canSend || busy) return
    setBusy(true)
    const message = [text.trim(), pictures.suffix()].filter(Boolean).join("\n\n")
    const sent = await ask.run(message, choice, selectedTarget, output)
    setBusy(false)
    if (sent) onClose()
  }
  // Insère à la position du curseur (ou à la place de la sélection), puis y replace le curseur.
  const put = (insertedText: string) => {
    const textAreaElement = textArea.current
    const selectionStart = textAreaElement?.selectionStart ?? text.length
    const selectionEnd = textAreaElement?.selectionEnd ?? text.length
    setText(text.slice(0, selectionStart) + insertedText + text.slice(selectionEnd))
    requestAnimationFrame(() => {
      const caretPosition = selectionStart + insertedText.length
      textAreaElement?.focus()
      textAreaElement?.setSelectionRange(caretPosition, caretPosition)
    })
  }
  // Une commande : son corps, sans en-tête ; son premier $ARGUMENTS est sélectionné pour être remplacé en tapant.
  const insert = (commandBody: string) => {
    const textAreaElement = textArea.current
    const selectionStart = textAreaElement?.selectionStart ?? text.length
    put(commandBody)
    const argument = argAt(commandBody)
    if (!argument) return
    const argumentStart = selectionStart + argument.at
    requestAnimationFrame(() => requestAnimationFrame(() => textAreaElement?.setSelectionRange(argumentStart, argumentStart + argument.len)))
  }
  // Une entrée de mémoire, citée telle quelle ; il reste à écrire la demande à la suite.
  const insertMemory = async (memoryFile: MemFile, scope: string) => {
    let memoryContent
    try {
      memoryContent = await api.memoryFile(memoryFile.key)
    } catch {
      toast.error(t("conversation.compose.memoryUnreadable"))
      return
    }
    const fence = memoryContent.text.includes("```") ? "~~~~" : "```"
    const isMemoryEntry = memoryFile.key.startsWith("m/")
    const where = isMemoryEntry ? t("conversation.compose.memoryScope", { scope, name: memoryFile.name }) : `${scope}/${memoryFile.name}`
    put(`${t("conversation.compose.memoryIntro", { where })}\n\n${fence}markdown\n${memoryContent.text.trim()}\n${fence}\n\n`)
  }
  const memItems = (files: MemFile[], scope: string) =>
    files.map((memoryFile) => (
      <DropdownMenuItem key={memoryFile.key} onSelect={() => insertMemory(memoryFile, scope)}>
        <FileText /> <span className="truncate">{memoryFile.name}</span>
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
          <span className="shrink-0 text-muted-foreground">{t("conversation.compose.output")}</span>
          <Choose
            value={output ?? ""}
            onChange={async (selectedOutput) => {
              const regeneratedText = await ask.reprompt?.(selectedOutput)
              if (regeneratedText === undefined) return
              setOutput(selectedOutput)
              setText(regeneratedText)
            }}
            options={ask.outputs.map((outputOption) => [outputOption.key, outputOption.label] as const)}
            label={t("conversation.compose.outputAria")}
            size="default"
            className="min-w-0 flex-1"
          />
        </div>
      )}
      {ask.targets && (
        <div className="flex items-center gap-2 text-sm">
          <span className="shrink-0 text-muted-foreground">{t("conversation.compose.folder")}</span>
          <Choose
            value={custom ? CUSTOM_TARGET_VALUE : String(target)}
            onChange={(selectedValue) => {
              if (selectedValue === CUSTOM_TARGET_VALUE) return
              setCustom(null)
              setTarget(Number(selectedValue))
            }}
            options={[
              ...ask.targets.map((targetOption, targetIndex) => [String(targetIndex), targetOption.label] as const),
              ...(custom ? [[CUSTOM_TARGET_VALUE, custom.label] as const] : []),
            ]}
            label={t("conversation.compose.newSessionFolder")}
            size="default"
            className="min-w-0 flex-1"
          />
          <Button
            type="button"
            variant="outline"
            onClick={async () => {
              const pickedDir = await actions.pickDir(t("conversation.compose.newSessionFolder"), selectedTarget?.project ?? actions.home)
              if (pickedDir) setCustom({ label: tilde(pickedDir, actions.home), project: pickedDir, dir: "", id: "", cwd: pickedDir })
            }}
          >
            <FolderSearch /> {t("conversation.compose.otherFolder")}
          </Button>
        </div>
      )}
      {mention.list}
      <textarea
        ref={textArea}
        value={text}
        onChange={(changeEvent) => {
          setText(changeEvent.target.value)
          mention.track(changeEvent.target)
        }}
        onKeyDown={(keyEvent) => {
          if (mention.onKey(keyEvent)) return
          if (!isSend(keyEvent)) return
          keyEvent.preventDefault()
          void send()
        }}
        onPaste={pictures.onPaste}
        onDrop={pictures.onDrop}
        placeholder={keyHint(t("conversation.compose.placeholder"))}
        autoFocus
        onFocus={(focusEvent) => {
          // Texte prérempli d'une commande : son $ARGUMENTS sélectionné ; sinon le curseur à la fin.
          const field = focusEvent.target
          const argument = argAt(field.value)
          if (argument) field.setSelectionRange(argument.at, argument.at + argument.len)
          else field.setSelectionRange(field.value.length, field.value.length)
        }}
        rows={8}
        maxLength={MAX_MESSAGE_LENGTH}
        className="session-text w-full resize-y rounded-lg border border-input bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/80"
        aria-label={t("conversation.compose.messageAria")}
      />
      <ImageChips images={pictures.images} busy={pictures.busy} onRemove={pictures.remove} />
      <div className="flex flex-wrap items-center gap-2">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm" disabled={!memory}>
              <Brain /> {t("conversation.compose.insertMemory")}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent onCloseAutoFocus={(focusEvent) => focusEvent.preventDefault()} align="start" className="max-h-80 w-80 overflow-y-auto">
            {ownProjects.map((memoryProject) => (
              <div key={memoryProject.dir}>
                <DropdownMenuLabel className="flex items-center gap-1.5">
                  <Brain className="size-3.5" /> {t("conversation.compose.thisProject")}
                </DropdownMenuLabel>
                {memItems(memoryProject.files, tilde(memoryProject.project, actions.home))}
                <DropdownMenuSeparator />
              </div>
            ))}
            {!!memory?.global.length && (
              <>
                <DropdownMenuLabel className="flex items-center gap-1.5">
                  <Globe className="size-3.5" /> {t("conversation.compose.global")}
                </DropdownMenuLabel>
                {memItems(memory.global, "~/.claude")}
              </>
            )}
            {otherProjects.map((memoryProject) => (
              <div key={memoryProject.dir}>
                <DropdownMenuSeparator />
                <DropdownMenuLabel className="truncate">{tilde(memoryProject.project, actions.home)}</DropdownMenuLabel>
                {memItems(memoryProject.files, tilde(memoryProject.project, actions.home))}
              </div>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        <SnippetMenu onInsert={insert} />
        <AttachButton onPick={pictures.pick} />
      </div>
      <DialogFooter className="items-stretch sm:items-center">
        <ModelPicker value={choice} onChange={setChoice} className="min-w-0 flex-nowrap sm:mr-auto" />
        <div className="flex items-center justify-end gap-2">
          <Button variant="outline" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button onClick={send} disabled={!canSend || busy} title={t("conversation.compose.sendTitle")}>
            <Send /> {ask.send}
          </Button>
        </div>
      </DialogFooter>
    </div>
  )
}
