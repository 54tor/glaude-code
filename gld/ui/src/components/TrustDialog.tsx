import { ShieldQuestion } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useTrustAsk } from "@/lib/trust"

// Le dialogue de confiance de Claude Code, posé ici en plein écran : la session attend la réponse.
export function TrustDialog() {
  const { t } = useTranslation()
  const trustQuestion = useTrustAsk()[0]
  return (
    <Dialog open={!!trustQuestion} onOpenChange={() => {}}>
      <DialogContent
        sheet={false}
        showCloseButton={false}
        onEscapeKeyDown={(escapeEvent) => escapeEvent.preventDefault()}
        onInteractOutside={(outsideEvent) => outsideEvent.preventDefault()}
        className="safe-t safe-b top-[var(--vvt,0px)] left-0 flex h-[var(--vvh,100dvh)] w-screen max-w-none translate-x-0 translate-y-0 flex-col items-center justify-center gap-6 rounded-none border-0 text-center sm:max-w-none"
      >
        <DialogHeader className="items-center sm:text-center">
          <ShieldQuestion className="size-12 text-warning" aria-hidden />
          <DialogTitle className="text-xl">{t("conversation.trust.title", { dir: trustQuestion?.label })}</DialogTitle>
          <DialogDescription className="max-w-prose">{t("conversation.trust.description")}</DialogDescription>
        </DialogHeader>
        <DialogFooter className="sm:justify-center">
          <Button variant="outline" onClick={() => trustQuestion?.answer(false)}>
            {t("conversation.trust.refuse")}
          </Button>
          <Button onClick={() => trustQuestion?.answer(true)}>{t("conversation.trust.accept")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
