import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Fan, Sparkles } from "lucide-react"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Choose } from "./Choose"
import { SnippetMenu } from "./SnippetMenu"
import { spliceAtCaret } from "@/lib/utils"

const MAX_INSTRUCTION_LENGTH = 8000

// Rédaction ou révision d'un SKILL.md par un modèle, à partir d'une consigne. Le texte revient en brouillon : rien n'est enregistré.
export function SkillWriter({ open, skillKey, name, onClose, onDraft }: { open: boolean; skillKey: string; name: string; onClose: () => void; onDraft: (text: string) => void }) {
  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="sm:max-w-2xl">{open && <Form skillKey={skillKey} name={name} onClose={onClose} onDraft={onDraft} />}</DialogContent>
    </Dialog>
  )
}

function Form({ skillKey, name, onClose, onDraft }: { skillKey: string; name: string; onClose: () => void; onDraft: (text: string) => void }) {
  const { t } = useTranslation()
  const models = [
    ["haiku", t("skills.writer.modelHaiku")],
    ["sonnet", "Sonnet"],
    ["opus", "Opus"],
    ["fable", "Fable"],
  ] as const
  const [instruction, setInstruction] = useState("")
  const [model, setModel] = useState("haiku")
  const [busy, setBusy] = useState(false)
  const instructionArea = useRef<HTMLTextAreaElement>(null)
  const insertSnippet = (text: string) => {
    setInstruction((currentInstruction) => spliceAtCaret(instructionArea.current, currentInstruction, text, MAX_INSTRUCTION_LENGTH))
    requestAnimationFrame(() => instructionArea.current?.focus())
  }
  const writeDraft = async () => {
    if (!instruction.trim() || busy) return
    setBusy(true)
    const draft = await api.skillDraft({ model, instruction, key: skillKey }).catch((error) => void toast.error(errMsg(error)))
    setBusy(false)
    if (!draft) return
    const costDescription = draft.cost ? t("skills.writer.cost", { cost: draft.cost.toFixed(4) }) : undefined
    toast.success(t("skills.writer.draftReady"), { description: costDescription })
    onClose()
    onDraft(draft.text)
  }
  const writeDraftOnCtrlEnter = (keyboardEvent: React.KeyboardEvent<HTMLTextAreaElement>) => {
    const isSubmitShortcut = (keyboardEvent.ctrlKey || keyboardEvent.metaKey) && keyboardEvent.key === "Enter"
    if (!isSubmitShortcut) return
    keyboardEvent.preventDefault()
    writeDraft()
  }
  return (
    <div className="grid min-w-0 gap-4">
      <DialogHeader>
        <DialogTitle>{t("skills.writer.title", { name })}</DialogTitle>
        <DialogDescription>{t("skills.writer.description")}</DialogDescription>
      </DialogHeader>
      <textarea
        ref={instructionArea}
        value={instruction}
        onChange={(changeEvent) => setInstruction(changeEvent.target.value)}
        onKeyDown={writeDraftOnCtrlEnter}
        placeholder={t("skills.writer.placeholder")}
        autoFocus
        rows={5}
        maxLength={MAX_INSTRUCTION_LENGTH}
        disabled={busy}
        className="session-text w-full resize-y rounded-lg border border-input bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/80"
        aria-label={t("skills.writer.instruction")}
      />
      <div className="flex">
        <SnippetMenu onInsert={insertSnippet} />
      </div>
      <div className="flex items-center gap-2 text-sm">
        {t("skills.writer.model")} <Choose value={model} onChange={setModel} options={models} label={t("skills.writer.model")} className="min-w-0" />
      </div>
      <DialogFooter>
        <Button variant="ghost" onClick={onClose} disabled={busy}>
          {t("common.cancel")}
        </Button>
        <Button onClick={writeDraft} disabled={!instruction.trim() || busy}>
          {busy ? <Fan className="animate-spin" /> : <Sparkles />} {t("skills.writer.write")}
        </Button>
      </DialogFooter>
    </div>
  )
}
