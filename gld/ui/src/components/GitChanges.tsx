import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { FileDiff } from "lucide-react"
import { api, type GitChange } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"

// Couleur de la lettre d'état : ajouté (vert), supprimé (rouge), conflit (rouge vif), non suivi (gris), modifié ou renommé (ambre).
export const tone = (statusCode: string) => {
  const isConflict = statusCode.includes("U") || statusCode === "AA" || statusCode === "DD"
  if (isConflict) return "text-destructive"
  if (statusCode === "??") return "text-muted-foreground"
  if (statusCode.includes("D")) return "text-destructive"
  if (statusCode.includes("A")) return "text-success"
  return "text-warning"
}

const Row = ({ onClick, ...rowProps }: React.ComponentProps<"div"> & { onClick?: () => void }) => {
  if (!onClick) return <div {...rowProps} />
  return (
    <button
      type="button"
      onClick={onClick}
      {...(rowProps as React.ComponentProps<"button">)}
      className={cn(rowProps.className, "rounded-sm hover:bg-accent")}
    />
  )
}

const stripPrefix = (path: string, prefix: string) => path.slice(prefix.length)

// Les fichiers en cours de modification d'un dépôt : un bouton ouvre la liste dans une fenêtre. La liste est lue à l'ouverture, et
// relue quand le nombre de changements annoncé par l'état git (relu toutes les 30 s) change. Le chemin du dépôt n'y est pas répété :
// il est déjà affiché à côté du bouton.
export function GitChanges({ dir, count, onOpen, prefix }: { dir: string; count: number; onOpen?: (path: string) => void; prefix?: string }) {
  const { t } = useTranslation()
  const [files, setFiles] = useState<GitChange[]>([])
  const [more, setMore] = useState(0)
  const [open, setOpen] = useState(false)
  useEffect(() => {
    if (!open || count === 0) return
    let isStillMounted = true
    api.gitChanges(dir).then(
      (changesResponse) => {
        if (!isStillMounted) return
        const pathPrefix = prefix ?? ""
        if (pathPrefix) {
          const filesUnderPrefix = changesResponse.files
            .filter((change) => change.path.startsWith(pathPrefix))
            .map((change) => {
              const renamedFromUnderPrefix = change.from?.startsWith(pathPrefix)
              return {
                ...change,
                path: stripPrefix(change.path, pathPrefix),
                from: renamedFromUnderPrefix && change.from ? stripPrefix(change.from, pathPrefix) : change.from,
              }
            })
          setFiles(filesUnderPrefix)
        } else {
          setFiles(changesResponse.files)
        }
        setMore(changesResponse.more ?? 0)
      },
      () => {}
    )
    return () => {
      isStillMounted = false
    }
  }, [dir, count, open, prefix])
  if (count === 0) return null
  const name = dir.split("/").filter(Boolean).pop() ?? dir
  const openChangedFile = (path: string) => {
    setOpen(false)
    onOpen?.(path)
  }
  return (
    <>
      <Button size="xs" variant="ghost" onClick={() => setOpen(true)} className="text-muted-foreground" title={t("conversation.gitChanges.viewTitle")}>
        <FileDiff /> {t("conversation.gitChanges.files", { count })}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{t("conversation.gitChanges.dialogTitle", { count })}</DialogTitle>
            <DialogDescription>{t("conversation.gitChanges.description", { name })}</DialogDescription>
          </DialogHeader>
          <ScrollArea className="max-h-[60vh] rounded-md border">
            <ul className="space-y-px bg-muted/40 p-1 font-mono text-xs">
              {files.map((change) => (
                <li key={change.path}>
                  <Row className="flex w-full items-baseline gap-2 px-1.5 py-0.5 text-left" onClick={onOpen && (() => openChangedFile(change.path))}>
                    <span className={cn("w-5 shrink-0 text-center font-semibold whitespace-pre", tone(change.status))} title={change.status}>
                      {change.status.trim() || " "}
                    </span>
                    <span className="min-w-0 flex-1 break-all" title={change.from ? t("conversation.gitChanges.renamedFrom", { from: change.from }) : undefined}>
                      {change.path}
                      {change.from && <span className="text-muted-foreground"> ← {change.from}</span>}
                    </span>
                  </Row>
                </li>
              ))}
              {more > 0 && <li className="px-1.5 py-0.5 text-muted-foreground">{t("conversation.gitChanges.more", { count: more })}</li>}
            </ul>
          </ScrollArea>
        </DialogContent>
      </Dialog>
    </>
  )
}
