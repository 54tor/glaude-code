import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import QRCode from "react-qr-code"
import { RefreshCw, Smartphone } from "lucide-react"
import { toast } from "sonner"
import { api, type Device } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { errMsg } from "@/lib/apiError"
import { ago } from "@/lib/format"
import { Button } from "@/components/ui/button"
import { Tip } from "@/components/ui/tooltip"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"

// Connecter un appareil : code à usage unique (pair.go), en chiffres à saisir sur « Connecter cet appareil » et en QR. Mode jeton : liste et révocation des appareils jumelés ;
// mode comptes : les sessions du profil en tiennent lieu.
// Adresse de bouclage : un autre appareil ne l'atteint pas. Une IP du réseau local, elle, marche telle quelle.
export const loopback = (url: string) => {
  const hostname = new URL(url, location.href).hostname
  return hostname === "localhost" || hostname.endsWith(".localhost") || hostname.startsWith("127.") || hostname === "[::1]"
}

const DEFAULT_CODE_TTL_SECONDS = 120
const MILLISECONDS_PER_SECOND = 1000
const CODE_GROUP_LENGTH = 4
const QR_SIZE = 200

export function TunnelHint({ origin }: { origin: string }) {
  const { t } = useTranslation()
  return (
    <div className="space-y-2 text-sm">
      <p>{t("shell.tunnel.loopback")}</p>
      <p className="text-muted-foreground">{t("shell.tunnel.lan")}</p>
      <p className="text-muted-foreground">{t("shell.tunnel.cloudflared")}</p>
      <pre className="overflow-x-auto rounded-md border bg-muted px-2 py-1 font-mono text-xs select-all">cloudflared tunnel --url {origin}</pre>
      <p className="text-warning">{t("shell.tunnel.warning")}</p>
    </div>
  )
}

export function PairButton() {
  const { t } = useTranslation()
  const auth = useAuth()
  const [open, setOpen] = useState(false)
  const [code, setCode] = useState<{ code: string; url: string; until: number } | null>(null)
  const [now, setNow] = useState(() => Date.now())
  const [devices, setDevices] = useState<Device[] | null>(null)
  const isTokenMode = auth.state === "open"

  const fresh = useCallback(async () => {
    try {
      const pairing = await api.pairCode()
      const ttlSeconds = pairing.ttl ?? DEFAULT_CODE_TTL_SECONDS
      setCode({ code: pairing.code, url: pairing.url, until: Date.now() + ttlSeconds * MILLISECONDS_PER_SECOND })
    } catch (pairError) {
      toast.error(errMsg(pairError) || t("shell.pair.failed"))
    }
  }, [t])
  const list = useCallback(() => {
    if (isTokenMode) api.devices().then((response) => setDevices(response.devices ?? []), () => setDevices([]))
  }, [isTokenMode])

  useEffect(() => {
    if (!open) return
    const clockInterval = setInterval(() => setNow(Date.now()), MILLISECONDS_PER_SECOND)
    return () => clearInterval(clockInterval)
  }, [open])
  const show = (isOpen: boolean) => {
    setOpen(isOpen)
    if (!isOpen) return
    void fresh()
    list()
  }

  const revoke = async (device: Device) => {
    try {
      await api.deviceRevoke(device.id)
      list()
    } catch (revokeError) {
      toast.error(errMsg(revokeError))
    }
  }

  if (auth.state !== "open" && auth.state !== "ok") return null
  const left = code ? Math.max(0, Math.ceil((code.until - now) / MILLISECONDS_PER_SECOND)) : 0
  const hasValidCode = code && left > 0
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
          {hasValidCode && (
            <p className="font-mono text-4xl font-semibold tracking-widest tabular-nums select-all" aria-label={t("shell.pair.code")}>
              {code.code.slice(0, CODE_GROUP_LENGTH)} {code.code.slice(CODE_GROUP_LENGTH)}
            </p>
          )}
          {hasValidCode && loopback(code.url) ? (
            <TunnelHint origin={new URL(code.url).origin} />
          ) : (
            <div className="grid size-56 place-items-center rounded-lg bg-white p-3">
              {hasValidCode ? (
                <QRCode value={code.url} size={QR_SIZE} level="M" title={t("shell.pair.qr")} />
              ) : (
                <Button variant="outline" size="sm" onClick={() => void fresh()}>
                  <RefreshCw /> {t("shell.pair.renew")}
                </Button>
              )}
            </div>
          )}
          <p className="text-sm text-muted-foreground" aria-live="polite">
            {hasValidCode ? t("shell.pair.expires", { s: left }) : t("shell.pair.expired")}
          </p>
        </div>

        <ol className="list-decimal space-y-1 pl-5 text-sm">
          <li>{t("shell.pair.step1")}</li>
          <li>{t("shell.pair.step2")}</li>
        </ol>

        {isTokenMode ? (
          <section className="space-y-2" aria-label={t("shell.pair.devices")}>
            <h3 className="text-sm font-semibold">{t("shell.pair.devices")}</h3>
            {devices?.length === 0 && <p className="text-sm text-muted-foreground">{t("shell.pair.none")}</p>}
            <ul className="divide-y rounded-lg border empty:hidden">
              {devices?.map((device) => (
                <li key={device.id} className="flex items-center gap-3 px-3 py-2 text-sm">
                  <span className="min-w-0 flex-1">
                    <span className="block truncate" title={device.name}>
                      {device.name || t("shell.pair.unknown")}
                    </span>
                    <span className="block text-xs text-muted-foreground">{t("shell.pair.seen", { ip: device.ip, ago: ago(device.seen) })}</span>
                  </span>
                  <Button variant="outline" size="sm" onClick={() => void revoke(device)}>
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
