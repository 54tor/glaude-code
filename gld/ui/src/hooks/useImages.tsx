import { hideHome } from "@/components/Message"
import { useEffect, useRef, useState } from "react"
import { FileText, Paperclip, X } from "lucide-react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { i18n } from "@/i18n"
import { authFetch } from "@/lib/auth"

export type Img = { path: string; url: string; name: string; image: boolean }

const OK = ["image/png", "image/jpeg", "image/gif", "image/webp"]

// Images collées (Ctrl+V) ou déposées dans un champ d'envoi : chacune est envoyée au serveur, qui la range dans un dossier privé
// et renvoie son chemin ; le message part avec « @chemin », que Claude Code sait lire. L'aperçu vient du fichier local (URL d'objet).
export function useImages(sid?: string) {
  const [images, setImages] = useState<Img[]>([])
  const [busy, setBusy] = useState(false)
  const live = useRef<string[]>([]) // URL d'objet à libérer en quittant
  useEffect(() => {
    const urls = live.current
    return () => urls.forEach((u) => URL.revokeObjectURL(u))
  }, [])

  const add = async (files: File[]) => {
    const pics = files
    if (!pics.length) return false
    setBusy(true)
    for (const f of pics) {
      try {
        const image = OK.includes(f.type)
        const headers: Record<string, string> = image ? { "Content-Type": f.type } : { "X-File-Name": encodeURIComponent(f.name) }
        const r = await authFetch((image ? "/image-upload" : "/file-upload") + (sid ? `?id=${encodeURIComponent(sid)}` : ""), { method: "POST", headers, body: f })
        const j = (await r.json()) as { ok: boolean; path?: string; out: string }
        if (!j.ok || !j.path) {
          toast.error(j.out)
          continue
        }
        const url = image ? URL.createObjectURL(f) : ""
        if (url) live.current.push(url)
        setImages((l) => [...l, { path: j.path!, url, image, name: f.name || i18n.t("shell.images.pasted") }])
      } catch (e) {
        toast.error(i18n.t("shell.images.uploadFailed", { msg: e instanceof Error ? e.message : String(e) }))
      }
    }
    setBusy(false)
    return true
  }
  const fromEvent = (data: DataTransfer | null) => Array.from(data?.files ?? [])
  return {
    images,
    busy,
    // Un collage de fichier n'insère pas de texte : on le prend, le reste suit son cours normal.
    onPaste: (e: React.ClipboardEvent) => {
      const files = fromEvent(e.clipboardData)
      if (files.length) {
        e.preventDefault()
        add(files)
      }
    },
    onDrop: (e: React.DragEvent) => {
      const files = fromEvent(e.dataTransfer)
      if (files.length) {
        e.preventDefault()
        add(files)
      }
    },
    pick: (files: File[]) => add(files),
    remove: (path: string) => setImages((l) => l.filter((i) => i.path !== path)),
    clear: () => setImages([]),
    // À ajouter au message : une mention « @chemin » par image. Sous Unix le home devient « ~ » (Claude Code le résout) : le terminal n'affiche pas l'identifiant du compte.
    suffix: () => images.map((i) => "@" + (i.path.startsWith("/") ? hideHome(i.path) : i.path)).join("\n"),
  }
}

export function ImageChips({ images, busy, onRemove }: { images: Img[]; busy: boolean; onRemove: (path: string) => void }) {
  const { t } = useTranslation()
  if (!images.length && !busy) return null
  return (
    <ul className="flex flex-wrap items-center gap-2" aria-label={t("shell.images.attached")}>
      {images.map((i) => (
        <li key={i.path} className="group relative">
          {i.image ? (
            <img src={i.url} alt={i.name} className="h-14 w-14 rounded-md border object-cover" />
          ) : (
            <span className="flex h-14 max-w-40 items-center gap-1.5 rounded-md border px-2 text-xs">
              <FileText className="size-4 shrink-0" />
              <span className="truncate">{i.name}</span>
            </span>
          )}
          <button
            type="button"
            onClick={() => onRemove(i.path)}
            className="absolute -top-1.5 -right-1.5 hidden rounded-full border bg-background p-0.5 shadow group-hover:block focus-visible:block"
            aria-label={t("shell.images.remove", { name: i.name })}
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
  const ref = useRef<HTMLInputElement>(null)
  return (
    <>
      <input ref={ref} type="file" multiple hidden onChange={(e) => (onPick(Array.from(e.target.files ?? [])), (e.target.value = ""))} />
      <button
        type="button"
        disabled={disabled}
        onClick={() => ref.current?.click()}
        title={t("shell.images.attach")}
        aria-label={t("shell.images.attach")}
        className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-50"
      >
        <Paperclip className="size-4" />
      </button>
    </>
  )
}
