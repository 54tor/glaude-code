import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Loader2, Sparkles } from "lucide-react"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Choose } from "./Choose"


// Rédaction ou révision d'un SKILL.md par un modèle, à partir d'une consigne. Le texte revient en brouillon : rien n'est enregistré.
export function SkillWriter({ open, skillKey, name, onClose, onDraft }: { open: boolean; skillKey: string; name: string; onClose: () => void; onDraft: (text: string) => void }) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
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
  const run = async () => {
    if (!instruction.trim() || busy) return
    setBusy(true)
    const r = (await api.post("/skill-draft", { model, instruction, key: skillKey })) as { ok: boolean; out: string; text?: string; cost?: number }
    setBusy(false)
    if (!r.ok || !r.text) return void toast.error(r.out)
    toast.success(t("skills.writer.draftReady"), { description: r.cost ? t("skills.writer.cost", { cost: r.cost.toFixed(4) }) : undefined })
    onClose()
    onDraft(r.text)
  }
  return (
    <div className="grid min-w-0 gap-4">
      <DialogHeader>
        <DialogTitle>{t("skills.writer.title", { name })}</DialogTitle>
        <DialogDescription>{t("skills.writer.description")}</DialogDescription>
      </DialogHeader>
      <textarea
        value={instruction}
        onChange={(e) => setInstruction(e.target.value)}
        onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "Enter" && (e.preventDefault(), run())}
        placeholder={t("skills.writer.placeholder")}
        autoFocus
        rows={5}
        maxLength={8000}
        disabled={busy}
        className="session-text w-full resize-y rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        aria-label={t("skills.writer.instruction")}
      />
      <div className="flex items-center gap-2 text-sm">
        {t("skills.writer.model")} <Choose value={model} onChange={setModel} options={models} label={t("skills.writer.model")} className="min-w-0" />
      </div>
      <DialogFooter>
        <Button variant="ghost" onClick={onClose} disabled={busy}>
          {t("common.cancel")}
        </Button>
        <Button onClick={run} disabled={!instruction.trim() || busy}>
          {busy ? <Loader2 className="animate-spin" /> : <Sparkles />} {t("skills.writer.write")}
        </Button>
      </DialogFooter>
    </div>
  )
}
