import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import QRCode from "react-qr-code"
import { RefreshCw, Smartphone } from "lucide-react"
import { toast } from "sonner"
import { api, type Device } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { ago } from "@/lib/format"
import { Button } from "@/components/ui/button"
import { Tip } from "@/components/ui/tooltip"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"

// Connecter un appareil : QR d'une adresse à code unique (pair.go). Mode jeton : liste et révocation des appareils jumelés ;
// mode comptes : les sessions du profil en tiennent lieu.
export function PairButton() {
  const { t } = useTranslation()
  const a = useAuth()
  const [open, setOpen] = useState(false)
  const [code, setCode] = useState<{ url: string; until: number } | null>(null)
  const [now, setNow] = useState(() => Date.now())
  const [devices, setDevices] = useState<Device[] | null>(null)
  const open_ = a.state === "open"

  const fresh = useCallback(async () => {
    const r = (await api.post("/pair-code", {})) as { ok: boolean; out?: string; url?: string; ttl?: number }
    if (!r.ok || !r.url) return void toast.error(r.out ?? t("shell.pair.failed"))
    setCode({ url: r.url, until: Date.now() + (r.ttl ?? 120) * 1000 })
  }, [t])
  const list = useCallback(() => {
    if (open_) api.devices().then((r) => setDevices(r.devices ?? []), () => setDevices([]))
  }, [open_])

  useEffect(() => {
    if (!open) return
    const i = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(i)
  }, [open])
  const show = (o: boolean) => {
    setOpen(o)
    if (!o) return
    void fresh()
    list()
  }

  const revoke = async (d: Device) => {
    const r = await api.post("/devices-revoke", { id: d.id })
    if (!r.ok) return void toast.error(r.out)
    list()
  }

  if (a.state !== "open" && a.state !== "ok") return null
  const left = code ? Math.max(0, Math.ceil((code.until - now) / 1000)) : 0
  return (
    <Dialog open={open} onOpenChange={show}>
      <Tip label={t("shell.pair.title")}>
        <DialogTrigger asChild>
          <Button variant="ghost" size="icon" className="size-9 text-muted-foreground" aria-label={t("shell.pair.title")}>
            <Smartphone className="size-6" />
          </Button>
        </DialogTrigger>
      </Tip>
      <DialogContent className="max-h-[90vh] gap-5 overflow-y-auto sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-xl">
            <Smartphone className="size-5 text-primary" aria-hidden /> {t("shell.pair.title")}
          </DialogTitle>
          <DialogDescription>{t("shell.pair.intro")}</DialogDescription>
        </DialogHeader>

        <div className="flex flex-col items-center gap-3">
          <div className="grid size-56 place-items-center rounded-lg bg-white p-3">
            {code && left > 0 ? (
              <QRCode value={code.url} size={200} level="M" title={t("shell.pair.qr")} />
            ) : (
              <Button variant="outline" size="sm" onClick={() => void fresh()}>
                <RefreshCw /> {t("shell.pair.renew")}
              </Button>
            )}
          </div>
          <p className="text-sm text-muted-foreground" aria-live="polite">
            {code && left > 0 ? t("shell.pair.expires", { s: left }) : t("shell.pair.expired")}
          </p>
        </div>

        <ol className="list-decimal space-y-1 pl-5 text-sm">
          <li>{t("shell.pair.step1")}</li>
          <li>{t("shell.pair.step2")}</li>
        </ol>

        {open_ ? (
          <section className="space-y-2" aria-label={t("shell.pair.devices")}>
            <h3 className="text-sm font-semibold">{t("shell.pair.devices")}</h3>
            {devices?.length === 0 && <p className="text-sm text-muted-foreground">{t("shell.pair.none")}</p>}
            <ul className="divide-y rounded-lg border empty:hidden">
              {devices?.map((d) => (
                <li key={d.id} className="flex items-center gap-3 px-3 py-2 text-sm">
                  <span className="min-w-0 flex-1">
                    <span className="block truncate" title={d.name}>
                      {d.name || t("shell.pair.unknown")}
                    </span>
                    <span className="block text-xs text-muted-foreground">{t("shell.pair.seen", { ip: d.ip, ago: ago(d.seen) })}</span>
                  </span>
                  <Button variant="outline" size="sm" onClick={() => void revoke(d)}>
                    {t("shell.pair.revoke")}
                  </Button>
                </li>
              ))}
            </ul>
          </section>
        ) : (
          <p className="text-sm text-muted-foreground">{t("shell.pair.accountNote")}</p>
        )}
      </DialogContent>
    </Dialog>
  )
}
