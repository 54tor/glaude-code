import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Download } from "lucide-react"
import { toast } from "sonner"
import { api, type DocEntry } from "@/lib/api"
import { qs, seg, v1 } from "@/lib/apiError"
import { brief, size } from "@/lib/format"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { WrittenDoc } from "./Message"

const BYTES_PER_KILOBYTE = 1024

function DocumentRow({ id, dir, document }: { id: string; dir: string; document: DocEntry }) {
  const { t } = useTranslation()
  const downloadUrl = v1(`/sessions/${seg(id)}/conversation/document${qs({ dir, path: document.path, download: true })}`)
  const isReadable = !document.missing && !document.tooBig
  const unreadableReason = document.missing ? t("conversation.documents.missing") : t("conversation.documents.tooBig")
  return (
    <li className="flex items-start gap-3 border-b py-2 last:border-b-0">
      <div className="min-w-0 flex-1">
        {isReadable ? (
          <WrittenDoc conv={{ id, dir }} path={document.path} />
        ) : (
          <div className="truncate text-xs text-muted-foreground" title={document.path}>
            {document.name} · {unreadableReason}
          </div>
        )}
        <div className="mt-0.5 truncate text-xs text-muted-foreground" title={document.path}>
          {!document.missing && `${size(Math.ceil(document.size / BYTES_PER_KILOBYTE))} · `}
          {document.ts ? brief(document.ts) : document.path}
        </div>
      </div>
      {isReadable && (
        <a
          href={downloadUrl}
          download={document.name}
          title={t("conversation.documents.download")}
          aria-label={t("conversation.documents.download")}
          className="mt-0.5 text-muted-foreground hover:text-foreground"
        >
          <Download className="size-4" aria-hidden />
        </a>
      )}
    </li>
  )
}

// Les fichiers affichables (md, html, pdf, images) que Claude a écrits ou cités dans une session, du plus récent au plus ancien.
export function DocumentsDialog({ id, dir, open, onClose }: { id: string; dir: string; open: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  const [documents, setDocuments] = useState<DocEntry[] | null>(null)
  useEffect(() => {
    if (!open || !id) return
    let isStillMounted = true
    setDocuments(null)
    api.sessionDocuments(id, dir).then(
      (documentsResponse) => isStillMounted && setDocuments(documentsResponse.documents),
      () => {
        if (!isStillMounted) return
        toast.error(t("conversation.documents.loadFail"))
        setDocuments([])
      }
    )
    return () => {
      isStillMounted = false
    }
  }, [open, id, dir, t])
  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="max-h-[90vh] min-w-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("conversation.documents.title")}</DialogTitle>
          <DialogDescription>{t("conversation.documents.description")}</DialogDescription>
        </DialogHeader>
        {documents !== null && documents.length === 0 && <p className="text-sm text-muted-foreground">{t("conversation.documents.empty")}</p>}
        {documents !== null && documents.length > 0 && (
          <ScrollArea className="min-h-0">
            <ul className="pr-3">
              {documents.map((document) => (
                <DocumentRow key={document.path} id={id} dir={dir} document={document} />
              ))}
            </ul>
          </ScrollArea>
        )}
      </DialogContent>
    </Dialog>
  )
}
