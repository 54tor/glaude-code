import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Share2 } from "lucide-react"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Confirm, type Ask } from "./Confirm"

type Info = { shared: boolean; mode?: "rw" | "ro"; connected: boolean }

// Partage d'une session par lien invité à usage unique (share.go) : l'hôte génère le lien, voit si un invité est là,
// bascule en lecture seule ou met fin au partage.
export function ShareDialog({ id, dir, title, onClose }: { id: string; dir: string; title?: string; onClose: () => void }) {
  const { t } = useTranslation()
  const [info, setInfo] = useState<Info | null>(null)
  const [failed, setFailed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [link, setLink] = useState("")
  const [ask, setAsk] = useState<Ask | null>(null)

  const refresh = useCallback(() => api.shareInfo(id).then((r) => (setInfo(r), setFailed(false)), () => setFailed(true)), [id])
  useEffect(() => {
    void refresh()
    const i = setInterval(() => void refresh(), 5000)
    return () => clearInterval(i)
  }, [refresh])

  const create = async () => {
    // le home se lit avant de créer : sans lui, le lien serait faux et le partage précédent déjà remplacé
    const h = await api.homes().catch(() => null)
    if (!h) return void toast.error(t("shell.share.homesFailed"))
    const r = await api.shareCreate(id, dir, title && title !== "(sans titre)" ? title.slice(0, 120) : undefined)
    if (!r.ok || !r.token) return void toast.error(t("shell.share.createFailed", { msg: r.out }))
    // « main » désigne le home du processus principal (share_guest.go)
    const url = `${location.origin}/invite/${encodeURIComponent(h.current || "main")}/${r.token}`
    setLink(url)
    try {
      await navigator.clipboard.writeText(url)
      toast.success(t("shell.share.copied"))
    } catch {
      toast.error(t("shell.share.copyFailed"))
    }
    void refresh()
  }
  const guard = (f: () => Promise<void>) => async () => {
    if (busy) return
    setBusy(true)
    try {
      await f()
    } finally {
      setBusy(false)
    }
  }
  const setMode = async (mode: "rw" | "ro") => {
    const r = await api.shareMode(id, mode)
    if (!r.ok) return void toast.error(t("shell.share.modeFailed", { msg: r.out }))
    void refresh()
  }
  const end = async () => {
    const r = await api.shareEnd(id)
    // le serveur révoque en mémoire même quand l'écriture échoue
    if (!r.ok) toast.error(t("shell.share.endSaveFailed", { msg: r.out }))
    setLink("")
    void refresh()
  }

  return (
    <>
      <Dialog open onOpenChange={(o) => !o && onClose()}>
        <DialogContent className="gap-4 sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Share2 className="size-5 text-primary" aria-hidden /> {t("shell.share.title")}
            </DialogTitle>
            <DialogDescription>{t("shell.share.intro")}</DialogDescription>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">{t("shell.share.singleUse")}</p>
          {location.protocol !== "https:" && <p className="text-sm text-warning">{t("shell.share.insecure")}</p>}
          {link && <input readOnly value={link} onFocus={(e) => e.target.select()} className="rounded-md border bg-muted px-2 py-1 font-mono text-xs" />}
          {info?.shared && <p className="text-sm">{info.connected ? t("shell.share.connected") : t("shell.share.notConnected")}</p>}
          {!info ? (
            failed ? (
              <div className="flex items-center gap-2 text-sm">
                <span>{t("shell.share.loadFailed")}</span>
                <Button size="sm" variant="outline" onClick={() => void refresh()}>
                  {t("shell.share.retry")}
                </Button>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">{t("shell.share.loading")}</p>
            )
          ) : (
            <div className="flex flex-wrap gap-2">
              {info.shared ? (
                <Button
                  disabled={busy}
                  onClick={() =>
                    setAsk({ title: t("shell.share.replaceTitle"), body: t("shell.share.replaceBody"), confirm: t("shell.share.replaceConfirm"), run: guard(create) })
                  }
                >
                  {t("shell.share.regenerate")}
                </Button>
              ) : (
                <Button disabled={busy} onClick={guard(create)}>
                  {t("shell.share.create")}
                </Button>
              )}
              {info.shared && (
                <Button variant="outline" disabled={busy} onClick={guard(() => setMode(info.mode === "ro" ? "rw" : "ro"))}>
                  {info.mode === "ro" ? t("shell.share.toRw") : t("shell.share.toRo")}
                </Button>
              )}
              {info.shared && (
                <Button
                  variant="destructive"
                  disabled={busy}
                  onClick={() => setAsk({ title: t("shell.share.endTitle"), body: t("shell.share.endBody"), confirm: t("shell.share.endConfirm"), run: guard(end) })}
                >
                  {t("shell.share.end")}
                </Button>
              )}
            </div>
          )}
        </DialogContent>
      </Dialog>
      <Confirm ask={ask} onClose={() => setAsk(null)} />
    </>
  )
}
