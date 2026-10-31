import { hideHome } from "@/components/Message"
import { useEffect, useRef, useState } from "react"
import { FileText, Paperclip, X } from "lucide-react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { i18n } from "@/i18n"
import { authFetch } from "@/lib/auth"
import { errMsg, qs, readResponse, v1 } from "@/lib/apiError"

export type Img = { path: string; url: string; name: string; image: boolean }

const PREVIEWABLE_IMAGE_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"]

const filesOf = (transfer: DataTransfer | null) => Array.from(transfer?.files ?? [])

// Sous Unix le home devient « ~ » (Claude Code le résout) : le terminal n'affiche pas l'identifiant du compte.
const mentionOf = (image: Img) => {
  const isAbsolutePath = image.path.startsWith("/")
  return "@" + (isAbsolutePath ? hideHome(image.path) : image.path)
}

// Images collées (Ctrl+V) ou déposées dans un champ d'envoi : chacune est envoyée au serveur, qui la range dans un dossier privé
// et renvoie son chemin ; le message part avec « @chemin », que Claude Code sait lire. L'aperçu vient du fichier local (URL d'objet).
export function useImages(sid?: string) {
  const [images, setImages] = useState<Img[]>([])
  const [busy, setBusy] = useState(false)
  const objectUrls = useRef<string[]>([]) // URL d'objet à libérer en quittant
  useEffect(() => {
    const urlsToRevoke = objectUrls.current
    return () => urlsToRevoke.forEach((objectUrl) => URL.revokeObjectURL(objectUrl))
  }, [])

  const uploadFile = async (file: File) => {
    const image = PREVIEWABLE_IMAGE_TYPES.includes(file.type)
    const headers: Record<string, string> = image ? { "Content-Type": file.type } : { "X-File-Name": encodeURIComponent(file.name) }
    const uploadKind = image ? "images" : "files"
    const response = await authFetch(v1(`/uploads/${uploadKind}${qs({ id: sid })}`), { method: "POST", headers, body: file })
    const uploaded = await readResponse<{ path: string }>(response)
    if (!uploaded.path) return
    const url = image ? URL.createObjectURL(file) : ""
    if (url) objectUrls.current.push(url)
    const name = file.name || i18n.t("shell.images.pasted")
    setImages((previousImages) => [...previousImages, { path: uploaded.path!, url, image, name }])
  }

  const add = async (files: File[]) => {
    if (!files.length) return false
    setBusy(true)
    for (const file of files) {
      try {
        await uploadFile(file)
      } catch (uploadError) {
        toast.error(i18n.t("shell.images.uploadFailed", { msg: errMsg(uploadError) }))
      }
    }
    setBusy(false)
    return true
  }
  return {
    images,
    busy,
    // Un collage de fichier n'insère pas de texte : on le prend, le reste suit son cours normal.
    onPaste: (event: React.ClipboardEvent) => {
      const files = filesOf(event.clipboardData)
      if (!files.length) return
      event.preventDefault()
      add(files)
    },
    onDrop: (event: React.DragEvent) => {
      const files = filesOf(event.dataTransfer)
      if (!files.length) return
      event.preventDefault()
      add(files)
    },
    pick: (files: File[]) => add(files),
    remove: (path: string) => setImages((previousImages) => previousImages.filter((image) => image.path !== path)),
    clear: () => setImages([]),
    // À ajouter au message : une mention « @chemin » par image.
    suffix: () => images.map(mentionOf).join("\n"),
  }
}

export function ImageChips({ images, busy, onRemove }: { images: Img[]; busy: boolean; onRemove: (path: string) => void }) {
  const { t } = useTranslation()
  if (!images.length && !busy) return null
  return (
    <ul className="flex flex-wrap items-center gap-2" aria-label={t("shell.images.attached")}>
      {images.map((image) => (
        <li key={image.path} className="group relative">
          {image.image ? (
            <img src={image.url} alt={image.name} className="h-14 w-14 rounded-md border object-cover" />
          ) : (
            <span className="flex h-14 max-w-40 items-center gap-1.5 rounded-md border px-2 text-xs">
              <FileText className="size-4 shrink-0" />
              <span className="truncate">{image.name}</span>
            </span>
          )}
          <button
            type="button"
            onClick={() => onRemove(image.path)}
            className="absolute -top-1.5 -right-1.5 hidden rounded-full border bg-background p-0.5 shadow group-hover:block focus-visible:block"
            aria-label={t("shell.images.remove", { name: image.name })}
          >
            <X className="size-3" />
          </button>
        </li>
      ))}
      {busy && <li className="text-xs text-muted-foreground">{t("shell.images.uploading")}</li>}
    </ul>
  )
}

// Bouton « joindre » : sur mobile, le collage et le glisser-déposer n'existent pas.
export function AttachButton({ onPick, disabled }: { onPick: (files: File[]) => void; disabled?: boolean }) {
  const { t } = useTranslation()
  const fileInput = useRef<HTMLInputElement>(null)
  const handleFilesChosen = (event: React.ChangeEvent<HTMLInputElement>) => {
    onPick(Array.from(event.target.files ?? []))
    event.target.value = ""
  }
  return (
    <>
      <input ref={fileInput} type="file" multiple hidden onChange={handleFilesChosen} />
      <button
        type="button"
        disabled={disabled}
        onClick={() => fileInput.current?.click()}
        title={t("shell.images.attach")}
        aria-label={t("shell.images.attach")}
        className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-50"
      >
        <Paperclip className="size-4" />
      </button>
    </>
  )
}
